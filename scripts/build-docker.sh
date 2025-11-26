#!/usr/bin/env bash
set -euo pipefail

# Read version from .version file
VERSION=$(sed 's/^\s*\|\s*$//g' .version || echo "dev")
# Get git revision (short commit hash)
REVISION=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Parse optional arguments
TAG="${1:-arcane:latest}"
PULL="${2:---pull}"

# Split PULL into array to allow multiple flags while preventing globbing
PULL_ARGS=()
if [ -n "$PULL" ]; then
  IFS=' ' read -r -a PULL_ARGS <<< "$PULL"
fi

echo "Building Docker image: ${TAG}"
echo "  VERSION: ${VERSION}"
echo "  REVISION: ${REVISION}"
echo ""

docker build "${PULL_ARGS[@]}" --rm \
  -f 'docker/Dockerfile' \
  --build-arg VERSION="${VERSION}" \
  --build-arg REVISION="${REVISION}" \
  -t "${TAG}" \
  .

echo ""
echo "✓ Build complete: ${TAG}"
