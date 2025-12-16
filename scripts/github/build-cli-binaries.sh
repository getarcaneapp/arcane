#!/usr/bin/env bash
set -euo pipefail

cd cli
mkdir -p .bin

# Prefer VERSION/REVISION from the environment (e.g., CI tag builds).
# Fall back to .arcane.json if available and jq is installed.
if [ -f ../.arcane.json ] && [ -z "${VERSION:-}" ]; then
  if command -v jq >/dev/null 2>&1; then
    VERSION=$(jq -r '.version' ../.arcane.json)
    REVISION=${REVISION:-$(jq -r '.revision // empty' ../.arcane.json)}
  else
    echo "Warning: jq not found; skipping version read from .arcane.json" >&2
  fi
fi

VERSION=${VERSION:-"dev"}
# Tags are typically like v1.2.3; the CLI version string expects 1.2.3.
VERSION=${VERSION#v}
REVISION=${REVISION:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}

LDFLAGS="-w -s -buildid=${VERSION} \
  -X github.com/getarcaneapp/arcane/cli/internal/config.Version=${VERSION} \
  -X github.com/getarcaneapp/arcane/cli/internal/config.Revision=${REVISION}"

BINARY_BASENAME="arcane-cli"

build_platform() {
  local target="$1" os="$2" arch="$3" arm_version="${4:-}"

  local ext=""
  if [ "$os" = "windows" ]; then
    ext=".exe"
  fi

  local output_path=".bin/${BINARY_BASENAME}-${target}${ext}"

  local cgo_enabled=0

  if [ -n "$arm_version" ]; then
    printf "Building %s (GOOS=%s GOARCH=%s GOARM=%s CGO_ENABLED=%s) ... " \
      "$output_path" "$os" "$arch" "$arm_version" "$cgo_enabled"

    GOARM="$arm_version" CGO_ENABLED="$cgo_enabled" GOOS="$os" GOARCH="$arch" \
      go build -ldflags="$LDFLAGS" -trimpath -o "$output_path" ./main.go
  else
    printf "Building %s (GOOS=%s GOARCH=%s CGO_ENABLED=%s) ... " \
      "$output_path" "$os" "$arch" "$cgo_enabled"

    CGO_ENABLED="$cgo_enabled" GOOS="$os" GOARCH="$arch" \
      go build -ldflags="$LDFLAGS" -trimpath -o "$output_path" ./main.go
  fi

  echo "Done"
}

echo "Version: ${VERSION}"
echo "Building Arcane CLI binaries for all platforms..."

# Linux
build_platform "linux-amd64" "linux" "amd64"
build_platform "linux-386"   "linux" "386"
build_platform "linux-arm64" "linux" "arm64"
build_platform "linux-armv7" "linux" "arm" "7"

# macOS (cross-compiled, CGO disabled)
build_platform "macos-x64"   "darwin" "amd64"
build_platform "macos-arm64" "darwin" "arm64"

# Windows
build_platform "windows-x64"   "windows" "amd64"
build_platform "windows-386"   "windows" "386"
build_platform "windows-arm64" "windows" "arm64"

echo "Compilation done"
