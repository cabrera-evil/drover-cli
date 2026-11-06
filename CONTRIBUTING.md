# Contributing

Thanks for contributing. Keep changes focused and the template minimal.

## Quick Checklist

1. Run tests: `make test`
2. Format code: `gofmt -w .`
3. Run lint (if available): `golangci-lint run ./...`
4. Update docs if behavior or structure changes

## Commit Style

Use Conventional Commits where possible:

- `feat:` new features
- `fix:` bug fixes
- `chore:` tooling or maintenance
- `docs:` documentation changes

## Project Structure

- `main.go` is the entry point.
- `internal/` holds application code.
- `configs/` contains example configs.
