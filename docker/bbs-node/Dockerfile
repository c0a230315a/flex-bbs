# syntax=docker/dockerfile:1

FROM golang:1.22-bookworm AS builder
WORKDIR /src

COPY backend-go/go.mod backend-go/go.sum ./backend-go/
RUN --mount=type=cache,target=/go/pkg/mod \
    cd backend-go && go mod download

COPY backend-go ./backend-go
RUN --mount=type=cache,target=/go/pkg/mod \
    cd backend-go && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/bbs-node ./cmd/bbs-node

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      jq \
      openjdk-17-jre-headless \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/bbs-node /app/bbs-node
COPY flexible-ipfs-base /app/flexible-ipfs-base

# Ensure runtime data isn't baked into the image even if it exists in the build context.
RUN rm -rf /app/flexible-ipfs-base/.ipfs /app/flexible-ipfs-base/getdata /app/flexible-ipfs-base/providers \
    && mkdir -p /data \
    && useradd -m -u 10001 app \
    && chown -R app:app /app /data

USER app
ENTRYPOINT ["/app/bbs-node"]

