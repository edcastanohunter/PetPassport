# syntax=docker/dockerfile:1.7
# Imagen de api/migrate/worker (docs/phase-1/09 §4): multi-stage, estática, distroless, no root.
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
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="petpassport-backend" \
      org.opencontainers.image.source="https://github.com/edcastanohunter/PetPassport" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=build /out/api /out/migrate /out/worker /
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/api"]
