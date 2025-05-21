# Glance with FreshRSS Widget Support

This is a fork of [Glance](https://github.com/glanceapp/glance) with added support for FreshRSS feeds integration.

## What's Added

This fork adds a new widget type `freshrss` that connects to your FreshRSS instance via its Fever API, retrieves your feeds, and displays them in the Glance dashboard.

## How to Use

Add a FreshRSS widget to your `glance.yml` file:

```yaml
- type: freshrss
  freshrss-url: http://your-freshrss-instance:port
  freshrss-user: your-username
  freshrss-api-pass: your-api-password
  limit: 10
  collapse-after: 5
  cache: 1h
```

### Requirements

1. A FreshRSS instance with the Fever API enabled
2. API password configured in your FreshRSS account settings

## Automated Workflows

This repository is configured with two GitHub Actions workflows:

### 1. Sync with Upstream Glance Releases

This workflow:
- Checks daily for new releases from the upstream Glance repository
- Automatically merges the upstream changes while preserving our FreshRSS widget code
- Creates a custom tag with format `v0.7.0-freshrss` to trigger the release workflow
- Only syncs if there's actually a new upstream release we haven't processed yet

### 2. Create Release

This workflow:
- Is triggered ONLY by our custom FreshRSS-tagged releases (e.g., `v0.7.0-freshrss`)
- Avoids rebuilding containers that upstream already built
- Creates a GitHub release with our FreshRSS-enabled version
- Builds and pushes Docker images with appropriate tags

## Using the Container

### Docker Compose Setup

The easiest way to run the container is with Docker Compose:

1. Replace `USERNAME` in the docker-compose.yml file with your GitHub username
2. Create your `glance.yml` configuration file in the same directory
3. Create a `data` directory for persistence
4. Run:

```bash
docker-compose up -d
```

### Running with Docker

If you prefer to run directly with Docker:

```bash
# Create a data directory
mkdir -p ./data

# Run the container
docker run -d \
  --name glance \
  -p 8080:8080 \
  -v $(pwd)/glance.yml:/app/config/glance.yml:ro \
  -v $(pwd)/data:/app/data \
  ghcr.io/USERNAME/glance:latest
```

Then access Glance at http://localhost:8080

## Manual Update Process

If you want to manually trigger a sync with the upstream repository:

1. Go to the "Actions" tab of this repository
2. Select the "Sync with Upstream Glance Releases" workflow
3. Click "Run workflow"
4. The workflow will check for new Glance releases, merge them, and trigger the release process

## Troubleshooting

### FreshRSS Widget Issues

If you encounter issues with the FreshRSS widget:

1. Make sure your FreshRSS instance is accessible from where Glance is running
2. Verify the Fever API is enabled in your FreshRSS settings
3. Double check your username and API password
4. Check the Glance logs for specific error messages 