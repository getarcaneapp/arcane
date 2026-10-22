set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list release

# Compute the next image version from unreleased commits; pass github-output to export it and a channel (next, beta)
[script('bash')]
version mode="" channel="next":
    set -euo pipefail

    CLIFF_CMD=""
    if command -v git-cliff &>/dev/null; then
        CLIFF_CMD="git-cliff"
    elif git cliff --version &>/dev/null; then
        CLIFF_CMD="git cliff"
    else
        echo "Error: git cliff is not installed. Please install it from https://git-cliff.org/docs/installation." >&2
        exit 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "Error: jq is required." >&2
        exit 1
    fi

    PREVIOUS_TAG=$(git tag -l 'v[0-9]*' --sort=-v:refname | head -n1)
    BASE_VERSION="${PREVIOUS_TAG#v}"
    if [ -z "$PREVIOUS_TAG" ]; then
        BASE_VERSION="0.0.0"
    fi

    CONTEXT=$($CLIFF_CMD --unreleased --context --offline --config cliff.toml)

    BREAKING=$(jq '[.[].commits[]? | select(.breaking == true)] | length' <<<"$CONTEXT")
    FEATURES=$(jq '[.[].commits[]? | select((.group // "") | test("New features"))] | length' <<<"$CONTEXT")

    IFS='.' read -r MAJOR MINOR PATCH <<<"$BASE_VERSION"
    if [ "$BREAKING" -gt 0 ]; then
        NEXT_BASE="$((MAJOR + 1)).0.0"
    elif [ "$FEATURES" -gt 0 ]; then
        NEXT_BASE="${MAJOR}.$((MINOR + 1)).0"
    else
        NEXT_BASE="${MAJOR}.${MINOR}.$((PATCH + 1))"
    fi

    GHCR_IMAGE="${GHCR_IMAGE:-getarcaneapp/manager}"
    if [ -n "${GHCR_TAGS+x}" ]; then
        TAGS="$GHCR_TAGS"
    else
        TOKEN=$(curl -fsSL "https://ghcr.io/token?scope=repository:${GHCR_IMAGE}:pull" | jq -r '.token')
        TAGS=""
        URL="https://ghcr.io/v2/${GHCR_IMAGE}/tags/list?n=1000"
        HEADERS_FILE=$(mktemp)
        while [ -n "$URL" ]; do
            PAGE=$(curl -fsSL -D "$HEADERS_FILE" -H "Authorization: Bearer ${TOKEN}" "$URL")
            TAGS+=$'\n'"$(jq -r '.tags[]?' <<<"$PAGE")"
            NEXT_LINK=$(awk -F'[<>]' 'tolower($0) ~ /^link:/ { print $2 }' "$HEADERS_FILE" | tr -d '\r')
            if [ -n "$NEXT_LINK" ]; then
                URL="https://ghcr.io${NEXT_LINK}"
            else
                URL=""
            fi
        done
        rm -f "$HEADERS_FILE"
    fi

    ESCAPED_BASE="${NEXT_BASE//./\\.}"
    MAX_N=$(grep -E "^v${ESCAPED_BASE}-{{ channel }}\.[0-9]+$" <<<"$TAGS" | sed -E 's/.*-{{ channel }}\.//' | sort -n | tail -n1 || true)
    COUNTER=$(( ${MAX_N:-0} + 1 ))

    VERSION="${NEXT_BASE}-{{ channel }}.${COUNTER}"
    IMAGE_TAG="v${VERSION}"

    echo "previous_tag=${PREVIOUS_TAG}"
    echo "version=${VERSION}"
    echo "image_tag=${IMAGE_TAG}"

    if [ "{{ mode }}" = "github-output" ]; then
        printf '%s\n' \
            "previous_tag=${PREVIOUS_TAG}" \
            "version=${VERSION}" \
            "image_tag=${IMAGE_TAG}" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set}"
    fi
