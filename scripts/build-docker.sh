#!/usr/bin/env bash
set -euo pipefail

# Read version and revision from .arcane.json file
if [ -f .arcane.json ]; then
  VERSION=$(jq -r '.version // "dev"' .arcane.json)
  REVISION=$(jq -r '.revision // empty' .arcane.json)
fi
VERSION=${VERSION:-"dev"}
REVISION=${REVISION:-$(git rev-parse --short HEAD 2>/dev/null || "unknown")}

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

depot build "${PULL_ARGS[@]}" --rm \
  -f 'docker/Dockerfile' \
  --build-arg VERSION="${VERSION}" \
  --build-arg REVISION="${REVISION}" \
  -t "${TAG}" \
  .

echo ""
echo "✓ Build complete: ${TAG}"
