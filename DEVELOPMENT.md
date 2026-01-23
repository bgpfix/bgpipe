# Development Guide

## Setting Up the Development Environment

This project uses a local copy of the [bgpfix](https://github.com/bgpfix/bgpfix) library for development, particularly for features that depend on in-development bgpfix functionality like BMP (BGP Monitoring Protocol) support.

### Quick Setup

Run the setup script to automatically clone the required bgpfix branch:

```bash
./scripts/setup-dev.sh
```

This script will:
1. Create a `.src` directory (gitignored)
2. Clone the bgpfix repository with the `dev0123` branch (which includes BMP support)
3. Run `go mod tidy` to update dependencies
4. Build the project

### Manual Setup

If you prefer to set up manually:

```bash
# Create the .src directory
mkdir -p .src

# Clone bgpfix with BMP support
cd .src
git clone --branch dev0123 https://github.com/bgpfix/bgpfix.git

# Return to project root and build
cd ..
go mod tidy
go build
```

### About the .src Directory

The `go.mod` file contains a replace directive:

```go
replace github.com/bgpfix/bgpfix => ./.src/bgpfix
```

This tells Go to use the local copy of bgpfix instead of the published version. This is necessary because:
- The `rv-live` stage requires BMP support from bgpfix
- BMP support is currently in development (see [bgpfix PR #14](https://github.com/bgpfix/bgpfix/pull/14))
- The published version of bgpfix doesn't yet include the BMP package

**Note:** This local development setup will no longer be needed once BMP support is merged and published in the official bgpfix release.

The `.src` directory is excluded from version control via `.gitignore`.

## Building and Testing

```bash
# Build the project
go build

# Run tests
go test ./...

# Run specific stage tests
go test ./stages/rv-live/...
```

## Working with rv-live

The `rv-live` stage reads BGP updates from RouteViews.org via Kafka in OpenBMP format. It requires:
- BMP support from bgpfix (in the `dev0123` branch)
- The Kafka consumer library (franz-go)

Example usage:

```bash
bgpipe -- rv-live --broker stream.routeviews.org:9092
```
