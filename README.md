# Go Template

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

## Updating

Release builds can update themselves from GitHub Releases:

```bash
go-template update --check          # report current vs latest, change nothing
go-template update                  # prompts "Update X -> Y? [y/N]"
go-template update --yes            # required when stdin is not interactive
go-template update --version v1.2.3 # install a specific release
```

Dev and dirty builds refuse to update. Set `GITHUB_TOKEN` for private repos or rate limits. Change `repoSlug` in `internal/app/update.go` when using this template.

## Docker

```bash
docker build -t go-template .
docker run --rm go-template
```

## Tooling

- `make` for build, run, test, tidy, and Docker targets
- `mise` for toolchain management and task aliases

## Development

See `DEVELOPMENT.md` for detailed workflows.
