# syntax=docker/dockerfile:1.7
# Imagen de api/migrate/worker/backup (docs/phase-1/09 §4): multi-stage, estática, distroless, no root.
FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup ./cmd/backup

# Typst para los PDF del worker (PP-F2-INF-004): binario estático musl, sin
# fuentes del sistema (usa las embebidas, licencia libre).
FROM alpine:3.20 AS typst
ARG TYPST_VERSION=0.13.1
RUN apk add --no-cache curl xz &&     curl -fsSL "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz"       | tar -xJ -C /tmp &&     install -m 0755 /tmp/typst-x86_64-unknown-linux-musl/typst /typst

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="petpassport-backend" \
      org.opencontainers.image.source="https://github.com/edcastanohunter/PetPassport" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=build /out/api /out/migrate /out/worker /out/backup /
COPY --from=typst /typst /usr/local/bin/typst
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/api"]
