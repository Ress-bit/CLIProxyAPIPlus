FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Auto-generate build date if not provided or set to "unknown"
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    if [ "${BUILD_DATE}" = "unknown" ] || [ -z "${BUILD_DATE}" ]; then \
        BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ); \
    fi && \
    if [ "${COMMIT}" = "none" ] || [ -z "${COMMIT}" ]; then \
        COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none"); \
    fi && \
    if [ "${VERSION}" = "dev" ] || [ -z "${VERSION}" ]; then \
        VERSION=$(git describe --tags --always 2>/dev/null || echo "dev"); \
    fi && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X 'main.Version=${VERSION}-plus' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" -o ./CLIProxyAPIPlus ./cmd/server/

FROM alpine:3.22.0

RUN apk add --no-cache tzdata

RUN mkdir /CLIProxyAPI

COPY --from=builder ./app/CLIProxyAPIPlus /CLIProxyAPI/CLIProxyAPIPlus

COPY config.example.yaml /CLIProxyAPI/config.example.yaml

COPY docker-entrypoint.sh /CLIProxyAPI/docker-entrypoint.sh

RUN chmod +x /CLIProxyAPI/docker-entrypoint.sh

WORKDIR /CLIProxyAPI

EXPOSE 8317

ENV TZ=Asia/Shanghai

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

ENTRYPOINT ["/CLIProxyAPI/docker-entrypoint.sh"]

CMD ["./CLIProxyAPIPlus"]