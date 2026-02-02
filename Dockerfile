FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first for better layer caching
# This layer will be cached unless dependencies change
COPY go.mod go.sum ./

# Download dependencies with retry and timeout handling
# Use go mod download with verification
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && \
    go mod verify

# Copy source code (this invalidates cache when code changes)
COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Build with optimizations and build cache
# -trimpath removes file system paths from binary for reproducible builds
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w -X 'main.Version=${VERSION}-plus' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
    -o ./CLIProxyAPIPlus \
    ./cmd/server/

# Runtime stage
FROM alpine:3.22.0

# Install runtime dependencies in one layer
RUN apk add --no-cache \
    tzdata \
    ca-certificates && \
    mkdir -p /CLIProxyAPI /data

# Copy binary from builder
COPY --from=builder /app/CLIProxyAPIPlus /CLIProxyAPI/CLIProxyAPIPlus

# Copy config template
COPY config.example.yaml /CLIProxyAPI/config.example.yaml

# Create entrypoint script in one layer
RUN printf '#!/bin/sh\n\
set -e\n\
\n\
# In cloud mode, ensure config.yaml exists in /data\n\
if [ "$DEPLOY" = "cloud" ]; then\n\
  if [ ! -f /data/config.yaml ]; then\n\
    echo "Cloud deploy: Initializing config.yaml from example..."\n\
    cp /CLIProxyAPI/config.example.yaml /data/config.yaml\n\
    echo "Config initialized at /data/config.yaml"\n\
  fi\n\
  # Create symlink to config in working directory\n\
  ln -sf /data/config.yaml /CLIProxyAPI/config.yaml\n\
fi\n\
\n\
exec "$@"\n' > /CLIProxyAPI/entrypoint.sh && \
    chmod +x /CLIProxyAPI/entrypoint.sh

WORKDIR /CLIProxyAPI

# Expose all required ports
EXPOSE 8317 8085 1455 54545 51121 11451

# Set timezone
ENV TZ=Asia/Shanghai
RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && \
    echo "${TZ}" > /etc/timezone

ENTRYPOINT ["/CLIProxyAPI/entrypoint.sh"]
CMD ["./CLIProxyAPIPlus"]