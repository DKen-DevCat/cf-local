# Phase 3-A.4.11: cf-local Control Plane (Go) イメージ。docker-compose の cf-local
# サービスがこれを build して起動する。
#
# 役割:
#   - `--config-dir` (default `/cf-local`, bind mount) を読んで AWS SDK 型に変換
#   - `--out-dir`    (default `/work/cf-local-conf`, named volume) に
#     `cf-local.conf` + `policies.json` を atomic rename で書き出す
#   - SIGINT/SIGTERM を待って idle (HTTP listener は A.5 で追加)

ARG GO_VERSION=1.26.1

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src

# 依存だけ先に解決して layer cache を効かせる。
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/      ./cmd/
COPY internal/ ./internal/

# CGO 不要 (BoltDB は Phase 4-A 以降。今は AWS SDK + 標準 lib のみ)。
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w" -o /out/cf-local ./cmd/cf-local

FROM alpine:3.20
# tini で PID 1 を取らせて signal 伝播を綺麗にしておく (idle 中の SIGTERM で即終了)。
RUN apk add --no-cache tini
COPY --from=builder /out/cf-local /usr/local/bin/cf-local

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/cf-local"]
