#!/usr/bin/env bash
set -euo pipefail

cd backend
mkdir -p .bin

HOST_OS="$(go env GOHOSTOS)"

# Read version and revision from .arcane.json
if [ -f ../.arcane.json ]; then
  VERSION=${VERSION:-$(jq -r '.version' ../.arcane.json)}
  REVISION=${REVISION:-$(jq -r '.revision // empty' ../.arcane.json)}
fi
VERSION=${VERSION:-"dev"}
REVISION=${REVISION:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}

LDFLAGS="-w -s -buildid=${VERSION} \
  -X github.com/getarcaneapp/arcane/backend/internal/config.Version=${VERSION} \
  -X github.com/getarcaneapp/arcane/backend/internal/config.Revision=${REVISION}"

DOCKER_ONLY=false
AGENT_BUILD=false
NEXT_BUILDS=false

for arg in "${@:-}"; do
  case "$arg" in
    --docker) DOCKER_ONLY=true ;;
    --agent)  AGENT_BUILD=true ;;
    --next-builds) NEXT_BUILDS=true ;;
    *) ;;
  esac
done

BINARY_BASENAME="arcane"
BUILD_TAGS=""
if [ "$AGENT_BUILD" = true ]; then
  BINARY_BASENAME="arcane-agent"
  BUILD_TAGS="exclude_frontend"
fi

build_platform() {
  local target="$1" os="$2" arch="$3" arm_version="${4:-}"

  # skip macOS builds on non-mac hosts (prevents clang -arch failure)
  if [ "$os" = "darwin" ] && [ "$HOST_OS" != "darwin" ]; then
    echo "Skipping $os/$arch (host=$HOST_OS, no macOS toolchain)"
    return 0
  fi

  local output_path=".bin/${BINARY_BASENAME}-${target}"

  local cgo_enabled=0
  if [ "$os" = "darwin" ] && [ "$HOST_OS" = "darwin" ]; then
    cgo_enabled="${CGO_ENABLED_DARWIN_OVERRIDE:-1}"
  fi

  if [ -n "$arm_version" ]; then
    printf "Building %s (GOOS=%s GOARCH=%s GOARM=%s CGO_ENABLED=%s)%s ... " \
      "$output_path" "$os" "$arch" "$arm_version" "$cgo_enabled" "${BUILD_TAGS:+ tags=$BUILD_TAGS}"
  else
    printf "Building %s (GOOS=%s GOARCH=%s CGO_ENABLED=%s)%s ... " \
      "$output_path" "$os" "$arch" "$cgo_enabled" "${BUILD_TAGS:+ tags=$BUILD_TAGS}"
  fi

  local build_flags=()
  [ -n "$BUILD_TAGS" ] && build_flags=(-tags "$BUILD_TAGS")

  if [ -n "$arm_version" ]; then
    GOARM="$arm_version" CGO_ENABLED="$cgo_enabled" GOOS="$os" GOARCH="$arch" \
      go build "${build_flags[@]}" -ldflags="$LDFLAGS" -trimpath -o "$output_path" ./cmd/main.go
  else
    CGO_ENABLED="$cgo_enabled" GOOS="$os" GOARCH="$arch" \
      go build "${build_flags[@]}" -ldflags="$LDFLAGS" -trimpath -o "$output_path" ./cmd/main.go
  fi
  echo "Done"
}

echo "Version: ${VERSION}"
if [ "$NEXT_BUILDS" = true ]; then
  echo "Building next images binaries (manager + agent for linux targets)..."
  # Build manager binaries
  BINARY_BASENAME="arcane"
  BUILD_TAGS=""
  build_platform "linux-amd64" "linux" "amd64"
  build_platform "linux-arm64" "linux" "arm64"
  build_platform "linux-armv7" "linux" "arm" "7"
  # Build agent binaries
  BINARY_BASENAME="arcane-agent"
  BUILD_TAGS="exclude_frontend"
  build_platform "linux-amd64" "linux" "amd64"
  build_platform "linux-arm64" "linux" "arm64"
  build_platform "linux-armv7" "linux" "arm" "7"
elif [ "$DOCKER_ONLY" = true ]; then
  if [ "$AGENT_BUILD" = true ]; then
    echo "Building agent binaries (docker-only linux targets)..."
  else
    echo "Building binaries (docker-only linux targets)..."
  fi
  build_platform "linux-amd64" "linux" "amd64"
  build_platform "linux-arm64" "linux" "arm64"
else
  if [ "$AGENT_BUILD" = true ]; then
    echo "Building agent binaries for all platforms..."
  else
    echo "Building binaries for all platforms..."
  fi
  build_platform "linux-amd64" "linux" "amd64"
  build_platform "linux-386"   "linux" "386"
  build_platform "linux-arm64" "linux" "arm64"
  build_platform "linux-armv7" "linux" "arm" "7"
  build_platform "macos-x64"   "darwin" "amd64"
  build_platform "macos-arm64" "darwin" "arm64"
fi

echo "Compilation done"
