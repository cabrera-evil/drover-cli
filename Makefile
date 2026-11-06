BINARY_NAME=drover
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-s -w -X main.Version=$(VERSION)
INSTALL_DIR?=$(HOME)/.local/bin

.PHONY: all build install install-completion run test lint clean tidy help docker-build docker-run

all: tidy test build

## Build the application
build:
	@echo "Building..."
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) .

## Install the binary into a user-level bin directory
install:
	@echo "Installing $(BINARY_NAME) into $(INSTALL_DIR)..."
	install -d "$(INSTALL_DIR)"
	go build -trimpath -ldflags "$(LDFLAGS)" -o "$(INSTALL_DIR)/$(BINARY_NAME)" .
	@$(MAKE) --no-print-directory install-completion

ZSH_COMP_DIR?=$(HOME)/.zsh/completions
BASH_COMP_DIR?=$(HOME)/.local/share/bash-completion/completions

## Install shell completions (zsh, bash) and make zsh load them
install-completion:
	@bin="$(INSTALL_DIR)/$(BINARY_NAME)"; \
	if "$$bin" completion zsh 2>/dev/null | grep -q compdef; then \
		install -d "$(ZSH_COMP_DIR)" "$(BASH_COMP_DIR)"; \
		"$$bin" completion zsh > "$(ZSH_COMP_DIR)/_$(BINARY_NAME)"; \
		"$$bin" completion bash > "$(BASH_COMP_DIR)/$(BINARY_NAME)"; \
		grep -qs '\.zsh/completions' "$(HOME)/.zshrc" || printf '\n# Completion path adjustments\nfpath=(~/.zsh/completions $$fpath)\nautoload -Uz compinit && compinit\n' >> "$(HOME)/.zshrc"; \
		echo "Completions installed; run 'exec zsh' (or 'exec bash') to load them."; \
	else \
		echo "$(BINARY_NAME) has no completion command; skipping completions."; \
	fi

## Run the application (pass ARGS="...")
run:
	@echo "Running..."
	go run . $(ARGS)

## Run tests
test:
	@echo "Running tests..."
	go test -v ./...

## Run golangci-lint
lint:
	golangci-lint run ./...

## Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/

## Tidy dependencies
tidy:
	@echo "Tidying modules..."
	go mod tidy

## Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY_NAME) .

## Run Docker image
docker-run:
	@echo "Running Docker image..."
	docker run --rm $(BINARY_NAME)

## Show help
help:
	@echo "Available targets:"
	@echo "  build        - Build the application"
	@echo "  install      - Install the binary into ~/.local/bin (override INSTALL_DIR)"
	@echo "  install-completion - Install zsh/bash completions and wire zsh fpath"
	@echo "  run          - Run the application (pass ARGS=\"...\")"
	@echo "  test         - Run tests"
	@echo "  lint         - Run golangci-lint"
	@echo "  clean        - Clean build artifacts"
	@echo "  tidy         - Tidy Go modules"
	@echo "  docker-build - Build the Docker image"
	@echo "  docker-run   - Run the Docker image"
	@echo "  all          - Run tidy, test, and build"
