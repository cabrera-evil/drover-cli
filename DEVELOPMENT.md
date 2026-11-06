# Development Guide

## Setup

Install toolchain with `mise`:

```bash
mise install
```

## Build

```bash
make build                     # bin/go-template
make install                   # ~/.local/bin/go-template (override INSTALL_DIR)
```

The build stamps `main.Version` from `git describe`; override with `make build VERSION=1.2.3`.

## Run

```bash
make run                       # go run .
make run ARGS="version"        # pass CLI arguments through
```

## Test

```bash
make test
```

## Lint

```bash
make lint                      # golangci-lint run ./...
```

## Docker

```bash
make docker-build
make docker-run
```

## Notes

- `APP_NAME` can be set to customize the greeting output.
- Update `configs/config.example.yaml` when you add new configuration options.
