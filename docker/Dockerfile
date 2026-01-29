# syntax=docker/dockerfile:1
ARG BUILD_TAGS=""

# Stage 1: Build Frontend
FROM --platform=$BUILDPLATFORM node:25-trixie-slim AS frontend-builder
RUN npm install -g --force corepack@v0.34.5 && corepack enable
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
ENV CI=true
WORKDIR /build

COPY pnpm-lock.yaml pnpm-workspace.yaml ./

RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm fetch

COPY package.json ./
COPY frontend ./frontend

RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install -r --offline

ENV BUILD_PATH=dist
RUN pnpm -C frontend build

# Stage 2: Build Backend
FROM --platform=$BUILDPLATFORM golang:1.25-trixie AS backend-builder
ARG TARGETARCH
ARG BUILD_TAGS
WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git tzdata jq \ 
&& apt-get clean && rm -rf /var/lib/apt/lists/*

COPY go.work ./
COPY types ./types
COPY cli ./cli
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /build/backend
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend ./
COPY .arcane.json ../arcane.json

COPY --from=frontend-builder /build/frontend/dist ./frontend/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    VERSION=$(jq -r '.version // "dev"' ../arcane.json) && \
    REVISION=$(jq -r '.revision // "unknown"' ../arcane.json) && \
    BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build \
    -tags "${BUILD_TAGS}" \
    -ldflags "-w -s -X github.com/getarcaneapp/arcane/backend/internal/config.Version=${VERSION} -X github.com/getarcaneapp/arcane/backend/internal/config.Revision=${REVISION} -X github.com/getarcaneapp/arcane/backend/internal/config.BuildTime=${BUILD_TIME}" \
    -trimpath \
    -o /build/arcane \
    ./cmd/main.go

# Stage 3: Production Image
FROM debian:trixie-slim AS runner
ARG TARGETARCH
ARG VERSION="dev"
ARG REVISION="unknown"

RUN apt-get update && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    vainfo clinfo libdrm2 libsystemd0 \
    && if [ "$TARGETARCH" = "amd64" ]; then \
        apt-get install -y --no-install-recommends intel-gpu-tools; \
    fi \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

ENV GIN_MODE=release
ENV PORT=3552
ENV ENVIRONMENT=production
ENV NVIDIA_VISIBLE_DEVICES=all \
    NVIDIA_DRIVER_CAPABILITIES=compute,utility \
    ROCR_VISIBLE_DEVICES=all \
    HIP_VISIBLE_DEVICES=all \
    ONEAPI_DEVICE_SELECTOR=level_zero:*,opencl:* \
    LD_LIBRARY_PATH=/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/opt/rocm/lib:/opt/intel/oneapi/lib:/opt/intel/oneapi/lib/intel64:/usr/lib/x86_64-linux-gnu:/usr/lib/aarch64-linux-gnu:/usr/lib64:/usr/lib:/lib64:/lib


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
com.getarcaneapp.arcane="true"

CMD ["./arcane"]
