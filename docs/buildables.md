This file will be moved to the website eventually. 

# Buildables

Buildables are compile-time optional features. They are only compiled when the `buildables` build tag is enabled and are then selectively activated via a comma-separated feature list embedded at build time.

## Overview

- Buildables are **compiled** with the `buildables` tag.
- Individual features are **enabled** via `buildables.EnabledFeatures` (set with `-ldflags`).
- Runtime checks use `buildables.HasBuildFeature("<feature>")`.
- When buildables are disabled, the buildable config fields are pruned and the feature logic is excluded.

## Enabling buildables

Build with the `buildables` tag and set enabled features via `-ldflags`:

- Build tag: `buildables`
- Enabled features: `github.com/getarcaneapp/arcane/backend/buildables.EnabledFeatures`

Example:

- `-tags=buildables`
- `-ldflags "-X github.com/getarcaneapp/arcane/backend/buildables.EnabledFeatures=autologin"`


## Selecting features

`EnabledFeatures` is a comma-separated list. Feature names are case-insensitive and trimmed.

Example:

- `autologin`
- `feature-a,feature-b,feature-c`

## Runtime checks

Use `buildables.HasBuildFeature("feature")` to gate execution paths and route registration.

## Configuration

Buildable-specific config fields live in `BuildablesConfig` and are only present when buildables are enabled. For example, the `autologin` feature uses:

- `AUTO_LOGIN_USERNAME`
- `AUTO_LOGIN_PASSWORD`

## Policy: default Docker images

**Do not ship buildables in default Docker images.**

- Default `docker/Dockerfile` and `docker/Dockerfile-agent` builds must **not** include the `buildables` tag.
- Default Dockerfiles must **not** contain any logic that injects `buildables` or sets `EnabledFeatures` (no tag auto-append, no build-time `-ldflags` for `EnabledFeatures`).
- Do **not** set `EnabledFeatures` in default Docker build pipelines.
- Buildable-enabled images must be created explicitly via a dedicated build pipeline or Dockerfile variant.

This ensures optional features are opt-in and excluded from standard releases.

## Custom Docker image example

Create a dedicated Dockerfile (do not modify the defaults) that builds with buildables and uses the official image as the runtime base:

```dockerfile
# docker/Dockerfile.buildables
FROM --platform=$BUILDPLATFORM golang:1.25-trixie AS builder
ARG TARGETARCH
ARG BUILD_TAGS="buildables"
ARG ENABLED_FEATURES="autologin"
ARG VERSION="dev"
ARG REVISION="unknown"

WORKDIR /build
COPY go.work ./
COPY types ./types
COPY cli ./cli
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /build/backend
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend ./

RUN --mount=type=cache,target=/root/.cache/go-build \
	BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
	CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build \
	-tags "${BUILD_TAGS}" \
	-ldflags "-w -s -X github.com/getarcaneapp/arcane/backend/internal/config.Version=${VERSION} \
		-X github.com/getarcaneapp/arcane/backend/internal/config.Revision=${REVISION} \
		-X github.com/getarcaneapp/arcane/backend/internal/config.BuildTime=${BUILD_TIME} \
		-X github.com/getarcaneapp/arcane/backend/buildables.EnabledFeatures=${ENABLED_FEATURES}" \
	-trimpath \
	-o /out/arcane \
	./cmd/main.go

FROM ghcr.io/getarcaneapp/arcane:latest
# For headless builds, use: ghcr.io/getarcaneapp/arcane-headless:latest
COPY --from=builder /out/arcane /app/arcane
```

Build it explicitly:

```bash
docker build -f docker/Dockerfile.buildables \
	--build-arg BUILD_TAGS=buildables \
	--build-arg ENABLED_FEATURES=autologin \
	-t arcane:buildables .
```

## Adding a new buildable feature

1. Guard feature entry points using `//go:build buildables` where appropriate.
2. Gate behavior with `buildables.HasBuildFeature("your-feature")`.
3. Add any buildable-only config to `BuildablesConfig`.
4. Ensure tests that use the feature build with the `buildables` tag.
