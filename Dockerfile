# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine3.23@sha256:5978cc992ad5ef96a7469713c8af849c1433824761ce3be2c56381403cd8d9a3 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=devel

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN test -n "${TARGETOS}" && test -n "${TARGETARCH}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w -X github.com/croutoncreations/sb-heartbeat/internal/cli.Version=${VERSION}" \
      -o /out/sb-heartbeat ./cmd/sb-heartbeat

FROM scratch

ARG VERSION=devel
LABEL org.opencontainers.image.title="SB Heartbeat" \
      org.opencontainers.image.description="Least-privilege Supabase project heartbeats" \
      org.opencontainers.image.source="https://github.com/croutoncreations/sb-heartbeat" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/sb-heartbeat /usr/local/bin/sb-heartbeat

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/sb-heartbeat"]
