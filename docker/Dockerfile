# syntax=docker/dockerfile:1

# Tags passed to "go build"
ARG BUILD_TAGS=""

# Stage 1: Build Frontend
FROM node:25-alpine AS frontend-builder
RUN npm install -g --force corepack && corepack enable
WORKDIR /build
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY frontend/package.json ./frontend/package.json
COPY frontend ./frontend
RUN --mount=type=cache,target=/root/.pnpm-store pnpm -C frontend install --frozen-lockfile

ENV BUILD_PATH=dist
RUN pnpm -C frontend build

# Stage 2: Build Backend
FROM golang:1.25-alpine AS backend-builder
ARG BUILD_TAGS
WORKDIR /build
RUN apk add --no-cache ca-certificates curl git tzdata

COPY ./.version ./VERSION
COPY ./backend/go.mod ./backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY ./backend ./
COPY --from=frontend-builder /build/frontend/dist ./frontend/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    VERSION=$(cat VERSION | tr -d '[:space:]') && \
    REVISION=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown") && \
    CGO_ENABLED=0 GOOS=linux \
    go build \
    -tags "${BUILD_TAGS}" \
    -ldflags "-w -s -X github.com/getarcaneapp/arcane-backend/internal/config.Version=${VERSION} -X github.com/getarcaneapp/arcane-backend/internal/config.Revision=${REVISION}" \
    -trimpath \
    -o /build/arcane \
    ./cmd/main.go

# Stage 3: Production Image
FROM alpine:3 AS runner
ARG VERSION="dev"
ARG REVISION="unknown"
RUN apk upgrade && apk --no-cache add ca-certificates curl tzdata gcompat
ENV GIN_MODE=release
ENV PORT=3552
ENV ENVIRONMENT=production
# Multi-distro, multi-arch library path for GPU support
# Includes paths for: NVIDIA runtime mounts, Debian/Ubuntu (AMD64/ARM64), RHEL/CentOS/Fedora, Arch, and standard fallbacks
# Covers: NVIDIA GPUs (all distros/architectures), future AMD ROCm support, and Intel GPU support
ENV LD_LIBRARY_PATH=/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/opt/rocm/lib:/usr/lib/x86_64-linux-gnu:/usr/lib/aarch64-linux-gnu:/usr/lib64:/usr/lib:/lib64:/lib
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=backend-builder /build/arcane .
EXPOSE 3552
VOLUME ["/app/data"]
LABEL org.opencontainers.image.authors="OFKM Technologies" \
org.opencontainers.image.url="https://github.com/getarcaneapp/arcane" \
org.opencontainers.image.documentation="https://github.com/getarcaneapp/arcane/blob/main/README.md" \
org.opencontainers.image.source="https://github.com/getarcaneapp/arcane" \
org.opencontainers.image.version=${VERSION} \
org.opencontainers.image.revision=${REVISION} \
org.opencontainers.image.licenses="BSD-3-Clause" \
org.opencontainers.image.ref.name="arcane" \
org.opencontainers.image.title="Arcane" \
org.opencontainers.image.description="Modern Docker Management, Made for Everyone" \
com.getarcaneapp.arcane.updater="false" \
com.getarcaneapp.arcane.server="true"
CMD ["./arcane"]