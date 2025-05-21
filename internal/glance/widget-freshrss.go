package glance

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type freshRSSWidget struct {
	widgetBase      `yaml:",inline"`
	Style           string           `yaml:"style"`
	ThumbnailHeight float64          `yaml:"thumbnail-height"`
	CardHeight      float64          `yaml:"card-height"`
	Items           rssFeedItemList  `yaml:"-"`
	Limit           int              `yaml:"limit"`
	CollapseAfter   int              `yaml:"collapse-after"`
	FreshRSSUrl     string           `yaml:"freshrss-url"`
	FreshRSSUser    string           `yaml:"freshrss-user"`
	FreshRSSApiPass string           `yaml:"freshrss-api-pass"`
}

type freshRssFeedsGroups struct {
	Group_id int
	Feed_ids string
}

type freshRssFeed struct {
	Id                   int
	Favicon_id           int
	Title                string
	Url                  string
	Site_url             string
	Is_spark             int
	Last_updated_on_time int
}

type freshRSSFeedsAPI struct {
	Api_version            uint
	Auth                   uint
	Last_refreshed_on_time int
	Feeds                  []freshRssFeed
	Feeds_groups           []freshRssFeedsGroups
}

func (widget *freshRSSWidget) initialize() error {
	widget.withTitle("FreshRSS Feed").withCacheDuration(1 * time.Hour)

	if widget.Limit <= 0 {
		widget.Limit = 25
	}

	if widget.CollapseAfter == 0 || widget.CollapseAfter < -1 {
		widget.CollapseAfter = 5
	}

	if widget.ThumbnailHeight < 0 {
		widget.ThumbnailHeight = 0
	}

	if widget.CardHeight < 0 {
		widget.CardHeight = 0
	}

	return nil
}

func (widget *freshRSSWidget) update(ctx context.Context) {
	var items rssFeedItemList
	var err error

	items, err = widget.getItemsFromFreshRssFeeds()

	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	if len(items) > widget.Limit {
		items = items[:widget.Limit]
	}

	widget.Items = items
}

func (widget *freshRSSWidget) Render() template.HTML {
	if widget.Style == "horizontal-cards" {
		return widget.renderTemplate(widget, rssWidgetHorizontalCardsTemplate)
	}

	if widget.Style == "horizontal-cards-2" {
		return widget.renderTemplate(widget, rssWidgetHorizontalCards2Template)
	}

	return widget.renderTemplate(widget, rssWidgetTemplate)
}

func (widget *freshRSSWidget) getItemsFromFreshRssFeeds() (rssFeedItemList, error) {
	var p freshRSSFeedsAPI
	var feedReqs []rssFeedRequest
	var param = url.Values{}

	user_credentials := []byte(fmt.Sprintf("%v:%v", widget.FreshRSSUser, widget.FreshRSSApiPass))
	api_key := fmt.Sprintf("%x", md5.Sum(user_credentials))

	param.Set("api_key", api_key)
	param.Set("feeds", "")
	var payload = bytes.NewBufferString(param.Encode())

	requestURL := fmt.Sprintf("%v/api/fever.php?api", widget.FreshRSSUrl)
	req, err := http.NewRequest(http.MethodPost, requestURL, payload)

	if err != nil {
		return nil, fmt.Errorf("could not create freshRss request: %v ", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("User-Agent", glanceUserAgentString)

	client := http.Client{
		Timeout: 10 * time.Second,
	}

	res, err := client.Do(req)
	if err != nil || res.StatusCode != 200 {
		return nil, fmt.Errorf("could not connect to freshRss instance: %v", err)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read freshRss response body: %v", err)
	}

	errr := json.Unmarshal(resBody, &p)
	if errr != nil {
		return nil, fmt.Errorf("could not unmarshal freshrss response body: %v", errr)
	}

	for i := range p.Feeds {
		var feedReq rssFeedRequest
		feedReq.URL = p.Feeds[i].Url
		feedReq.Title = p.Feeds[i].Title
		feedReqs = append(feedReqs, feedReq)
	}

	job := newJob(widget.fetchItemsFromFeedTask, feedReqs).withWorkers(30)
	feeds, errs, err := workerPoolDo(job)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNoContent, err)
	}

	failed := 0
	entries := make(rssFeedItemList, 0, len(feeds)*10)
	seen := make(map[string]struct{})

	for i := range feeds {
		if errs[i] != nil {
			failed++
			slog.Error("Failed to get RSS feed", "url", feedReqs[i].URL, "error", errs[i])
			continue
		}

		for _, item := range feeds[i] {
			if _, exists := seen[item.Link]; exists {
				continue
			}
			entries = append(entries, item)
			seen[item.Link] = struct{}{}
		}
	}

	if failed == len(feedReqs) {
		return nil, errNoContent
	}

	if failed > 0 {
		return entries, fmt.Errorf("%w: missing %d RSS feeds", errPartialContent, failed)
	}

	entries.sortByNewest()
	return entries, nil
}

func (widget *freshRSSWidget) fetchItemsFromFeedTask(request rssFeedRequest) ([]rssFeedItem, error) {
	req, err := http.NewRequest("GET", request.URL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", glanceUserAgentString)

	for key, value := range request.Headers {
		req.Header.Set(key, value)
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, request.URL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	feed, err := feedParser.ParseString(string(body))
	if err != nil {
		return nil, err
	}

	if request.Limit > 0 && len(feed.Items) > request.Limit {
		feed.Items = feed.Items[:request.Limit]
	}

	items := make(rssFeedItemList, 0, len(feed.Items))

	for i := range feed.Items {
		item := feed.Items[i]

		rssItem := rssFeedItem{
			ChannelURL: feed.Link,
		}

		if request.ItemLinkPrefix != "" {
			rssItem.Link = request.ItemLinkPrefix + item.Link
		} else if strings.HasPrefix(item.Link, "http://") || strings.HasPrefix(item.Link, "https://") {
			rssItem.Link = item.Link
		} else {
			parsedUrl, err := url.Parse(feed.Link)
			if err != nil {
				parsedUrl, err = url.Parse(request.URL)
			}

			if err == nil {
				var link string

				if len(item.Link) > 0 && item.Link[0] == '/' {
					link = item.Link
				} else {
					link = "/" + item.Link
				}

				rssItem.Link = parsedUrl.Scheme + "://" + parsedUrl.Host + link
			}
		}

		if item.Title != "" {
			rssItem.Title = html.UnescapeString(item.Title)
		} else {
			rssItem.Title = shortenFeedDescriptionLen(item.Description, 100)
		}

		if !request.HideDescription && item.Description != "" && item.Title != "" {
			rssItem.Description = shortenFeedDescriptionLen(item.Description, 200)
		}

		if !request.HideCategories {
			var categories = make([]string, 0, 6)

			for _, category := range item.Categories {
				if len(categories) == 6 {
					break
				}

				if len(category) == 0 || len(category) > 30 {
					continue
				}

				categories = append(categories, category)
			}

			rssItem.Categories = categories
		}

		if request.Title != "" {
			rssItem.ChannelName = request.Title
		} else {
			rssItem.ChannelName = feed.Title
		}

		if item.Image != nil {
			rssItem.ImageURL = item.Image.URL
		} else if url := findThumbnailInItemExtensions(item); url != "" {
			rssItem.ImageURL = url
		} else if feed.Image != nil {
			if len(feed.Image.URL) > 0 && feed.Image.URL[0] == '/' {
				rssItem.ImageURL = strings.TrimRight(feed.Link, "/") + feed.Image.URL
			} else {
				rssItem.ImageURL = feed.Image.URL
			}
		}

		if item.PublishedParsed != nil {
			rssItem.PublishedAt = *item.PublishedParsed
		} else {
			rssItem.PublishedAt = time.Now()
		}

		items = append(items, rssItem)
	}

	return items, nil
} 