set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list types

# Validate a types module version against the latest types/v3 tag; pass github-output to export the result
[positional-arguments]
[script('bash')]
check version mode="":
    set -euo pipefail

    VERSION="$1"
    if ! [[ "$VERSION" =~ ^v3\.[0-9]+\.[0-9]+$ ]]; then
        echo "Error: version must match v3.MINOR.PATCH (got '$VERSION')." >&2
        exit 1
    fi

    if git rev-parse -q --verify "refs/tags/types/$VERSION" >/dev/null; then
        echo "Error: tag types/$VERSION already exists." >&2
        exit 1
    fi

    PREVIOUS_TAG=$(git tag -l 'types/v3.*' --sort=-v:refname | head -n1)
    PUBLISH=true
    if [ -z "$PREVIOUS_TAG" ]; then
        if [ "$VERSION" != "v3.0.0" ]; then
            echo "Error: the initial types/v3 release must be v3.0.0 (got '$VERSION')." >&2
            exit 1
        fi
    else
        PREVIOUS_VERSION="${PREVIOUS_TAG#types/}"
        if [ "$(printf '%s\n%s\n' "$PREVIOUS_VERSION" "$VERSION" | sort -V | tail -n1)" != "$VERSION" ]; then
            echo "Error: $VERSION must be greater than the latest types release $PREVIOUS_VERSION." >&2
            exit 1
        fi
        if git diff --quiet "$PREVIOUS_TAG" HEAD -- types/; then
            PUBLISH=false
            echo "::notice::types/ is unchanged since $PREVIOUS_TAG; nothing to publish."
        fi
    fi

    echo "previous_tag=${PREVIOUS_TAG}"
    echo "publish=${PUBLISH}"

    if [ "${2:-}" = "github-output" ]; then
        printf '%s\n' \
            "previous_tag=${PREVIOUS_TAG}" \
            "publish=${PUBLISH}" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set}"
    fi

# Verify the types module and build its workspace consumers
verify:
    cd types && go mod verify && go build ./... && go vet ./...
    cd backend && go build -tags exclude_frontend,buildables ./...
    cd cli && go build ./...

# Create the types/<version> tag and a non-latest GitHub release with notes for types/ commits since previous_tag
[positional-arguments]
[script('bash')]
publish version previous_tag="":
    set -euo pipefail

    VERSION="$1"
    PREVIOUS_TAG="${2:-$(git tag -l 'types/v2.*' --sort=-v:refname | head -n 1)}"
    NOTES=$(git log --format='- %s (%h)' "${PREVIOUS_TAG:+$PREVIOUS_TAG..}HEAD" -- types/)

    gh release create "types/$VERSION" \
        --target "$(git rev-parse HEAD)" \
        --title "types/$VERSION" \
        --notes "${NOTES:-No changes.}" \
        --latest=false
