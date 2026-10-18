# syntax=docker/dockerfile:1
# Multi-stage build: static Go binary on distroless, non-root, no shell.
# Base images are tracked by Dependabot. Pin them by digest (@sha256:...) once the
# first build works.

FROM golang:1.27-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download && go mod verify

COPY . .
# Pinned Tailwind CLI (checksum verified), then CSS/JS bundles with hashed names
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run ./tools/fetchtools && \
    go tool templ generate && \
    go run ./tools/assetbuild

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -tags nodynamic \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/zaytona ./cmd/zaytona

# Writable uploads dir owned by the distroless nonroot user (65532)
RUN mkdir -p /out/uploads && chown 65532:65532 /out/uploads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/zaytona /zaytona
COPY --from=build --chown=65532:65532 /out/uploads /data/uploads

ENV APP_ENV=production \
    PORT=3000 \
    UPLOAD_DIR=/data/uploads
USER nonroot:nonroot
EXPOSE 3000
VOLUME ["/data/uploads"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/zaytona", "healthcheck"]
ENTRYPOINT ["/zaytona"]
CMD ["serve"]
