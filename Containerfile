# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/zip-forger ./cmd/zip-forger

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S zipforger && \
    adduser -S -G zipforger -h /var/lib/zip-forger zipforger && \
    mkdir -p /var/lib/zip-forger/cache && \
    chown -R zipforger:zipforger /var/lib/zip-forger

COPY --from=build /out/zip-forger /usr/local/bin/zip-forger

USER zipforger

ENV ZIP_FORGER_ADDR=:8080 \
    ZIP_FORGER_CACHE_DIR=/var/lib/zip-forger/cache

EXPOSE 8080

VOLUME ["/var/lib/zip-forger/cache"]

ENTRYPOINT ["/usr/local/bin/zip-forger"]
