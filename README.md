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

## Updating

Release builds can update themselves from GitHub Releases:

```bash
drover update --check          # report current vs latest, change nothing
drover update                  # interactive: spinner, "Update X -> Y? [y/N]", progress, result
drover update --yes            # skip the prompt; required when stdin is not interactive
drover update --version v1.2.3 # install a specific release
```

In a terminal, `update` starts an interactive flow automatically (unless `--yes` or `--check` is set); when stdin or stdout is not a terminal it keeps the plain scripted behavior and refuses to update without `--yes`.

While the binary is being replaced, key presses and the first external SIGINT/SIGTERM are ignored; a second external SIGINT/SIGTERM ends the process. In the interactive view Ctrl-C is a key press, not a signal, so to stop a hung replace use `kill` from another shell.

Dev and dirty builds refuse to update. Set `GITHUB_TOKEN` for private repos or rate limits.

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
