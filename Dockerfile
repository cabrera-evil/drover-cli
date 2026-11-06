# syntax=docker/dockerfile:1.7

# Use specific Go version for consistency
ARG GO_VERSION=1.25

# ==============================================================================
# Base stage - Common build image setup
# ==============================================================================
FROM golang:${GO_VERSION}-bookworm AS base

WORKDIR /src
ENV CGO_ENABLED=0

# ==============================================================================
# Dependencies stage - Download and cache Go modules
# ==============================================================================
FROM base AS dependencies

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# ==============================================================================
# Build stage - Compile the application
# ==============================================================================
FROM dependencies AS builder

ARG VERSION=dev
COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /out/go-template .

# ==============================================================================
# Runtime stage - Minimal production image
# ==============================================================================
FROM debian:bookworm-slim AS runtime

# Install runtime dependencies
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    dumb-init && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/* /tmp/* /var/tmp/*

# Switch to non-root user
RUN groupadd --system appuser && \
    useradd --system --gid appuser --create-home --home-dir /app appuser
USER appuser
WORKDIR /app

# Copy application binary
COPY --from=builder /out/go-template /app/go-template

# Use dumb-init to handle signals properly
ENTRYPOINT ["dumb-init", "--"]

# Start the application
CMD ["/app/go-template"]
