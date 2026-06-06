# syntax=docker/dockerfile:1
ARG BUILD_TAGS=""
ARG ARCANE_RUNTIME_BASE_IMAGE=ghcr.io/getarcaneapp/base:distroless

# Stage 1: Build Frontend
FROM --platform=$BUILDPLATFORM node:26-trixie-slim AS frontend-builder
RUN npm install -g --force corepack@v0.34.5 && corepack enable
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
ENV CI=true
WORKDIR /build

COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY frontend/package.json ./frontend/package.json

RUN --mount=type=cache,id=arcane-pnpm-v2,target=/pnpm/store,sharing=locked \
    pnpm fetch && \
    pnpm install -r --offline --frozen-lockfile

RUN printf 'NODE_VERSION=%s\nSVELTEKIT_VERSION=%s\n' \
    "$(node --version)" \
    "$(pnpm -C frontend exec node -p 'require("@sveltejs/kit/package.json").version')" \
    > /tmp/arcane-frontend-build.env

COPY frontend ./frontend

ENV BUILD_PATH=dist
ENV NODE_OPTIONS="--max-old-space-size=4096"
RUN pnpm -C frontend build

# Stage 2: Build Backend
FROM --platform=$BUILDPLATFORM golang:1.26.4-trixie AS backend-builder
ARG TARGETARCH
ARG TARGETVARIANT
ARG BUILD_TAGS
ARG VERSION="dev"
ARG REVISION="unknown"
ARG BUILD_TIME="unknown"
WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git tzdata jq \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY go.work ./
COPY types ./types
COPY cli ./cli
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /build/backend
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY backend ./
COPY .arcane.json ../arcane.json

COPY --from=frontend-builder /build/frontend/dist ./frontend/dist
COPY --from=frontend-builder /tmp/arcane-frontend-build.env /tmp/arcane-frontend-build.env
RUN --mount=type=cache,target=/root/.cache/go-build \
    . /tmp/arcane-frontend-build.env && \
    if [ "${VERSION}" = "dev" ]; then VERSION=$(jq -r '.version // "dev"' ../arcane.json); fi && \
    if [ "${REVISION}" = "unknown" ]; then REVISION=$(jq -r '.revision // "unknown"' ../arcane.json); fi && \
    if [ "${BUILD_TIME}" = "unknown" ]; then BUILD_TIME=$(jq -r '.buildTime // "unknown"' ../arcane.json); fi && \
    export GOARCH="${TARGETARCH}" && \
    if [ "${TARGETARCH}" = "arm" ]; then \
        GOARM="${TARGETVARIANT#v}"; \
        if [ -z "${GOARM}" ]; then GOARM="7"; fi; \
        export GOARM; \
    fi && \
    CGO_ENABLED=0 GOOS=linux \
    go build \
    -tags "${BUILD_TAGS}" \
    -ldflags "-w -s -X github.com/getarcaneapp/arcane/backend/internal/config.Version=${VERSION} -X github.com/getarcaneapp/arcane/backend/internal/config.Revision=${REVISION} -X github.com/getarcaneapp/arcane/backend/internal/config.BuildTime=${BUILD_TIME} -X github.com/getarcaneapp/arcane/backend/internal/config.NodeVersion=${NODE_VERSION} -X github.com/getarcaneapp/arcane/backend/internal/config.SvelteKitVersion=${SVELTEKIT_VERSION}" \
    -trimpath \
    -o /build/arcane \
    ./cmd/main.go

RUN mkdir -p /build/rootfs/app/data /build/rootfs/builds

# Stage 3: Hardened Production Image
FROM ${ARCANE_RUNTIME_BASE_IMAGE} AS runner-hardened

ARG TARGETARCH
ARG VERSION
ARG REVISION

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

# The container starts as "root", but arcane drops to the requetsed PUID and PGID after the runtime is initialized 
USER 0:0
COPY --from=backend-builder --chown=65532:65532 /build/rootfs/app/ /app/
COPY --from=backend-builder --chown=65532:65532 /build/rootfs/builds/ /builds/
COPY --from=backend-builder --chown=65532:65532 /build/arcane .
EXPOSE 3552
VOLUME ["/app/data"]

LABEL com.getarcaneapp.arcane="true"

CMD ["./arcane"]
