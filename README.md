# drover

## Overview

A minimal Go starter with a clean layout and basic tooling. The app prints a simple greeting and is meant to be a reusable base for new services or CLIs.

## Structure

- `main.go` boots the app.
- `internal/app/` holds application logic.
- `internal/config/` centralizes configuration loading.
- `configs/` contains example configuration files.
- `bin/` is the build output directory.

## Requirements

- Go 1.25+ (or use `mise` to install)

## Quick Start

```bash
make run
```

Optional configuration:

```bash
APP_NAME=Codex make run
```

## Docker

```bash
docker build -t drover .
docker run --rm drover
```

## Tooling

- `make` for build, run, test, tidy, and Docker targets
- `mise` for toolchain management and task aliases

## Development

See `DEVELOPMENT.md` for detailed workflows.
