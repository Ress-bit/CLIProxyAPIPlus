FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Auto-generate build info if not provided or set to defaults
# Note: .git is excluded in .dockerignore, so we generate fallback values
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    FINAL_BUILD_DATE="${BUILD_DATE}"; \
    FINAL_COMMIT="${COMMIT}"; \
    FINAL_VERSION="${VERSION}"; \
    if [ "${FINAL_BUILD_DATE}" = "unknown" ] || [ -z "${FINAL_BUILD_DATE}" ]; then \
        FINAL_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ); \
    fi && \
    if [ "${FINAL_COMMIT}" = "none" ] || [ -z "${FINAL_COMMIT}" ]; then \
        FINAL_COMMIT=$(date -u +%Y%m%d%H%M%S); \
    fi && \
    if [ "${FINAL_VERSION}" = "dev" ] || [ -z "${FINAL_VERSION}" ]; then \
        FINAL_VERSION="docker-$(date -u +%Y%m%d)"; \
    fi && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X 'main.Version=${FINAL_VERSION}-plus' -X 'main.Commit=${FINAL_COMMIT}' -X 'main.BuildDate=${FINAL_BUILD_DATE}'" -o ./CLIProxyAPIPlus ./cmd/server/

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