set working-directory := './'

edge_proto_dir := 'backend/pkg/libarcane/edge/proto'

_default:
    @just --list

# Run frontend dev server on port 3000
[group('dev')]
_dev-frontend:
    pnpm -C frontend dev

# Run backend with hot reload on port 3552
[group('dev')]
_dev-backend:
    cd backend && air

[group('dev')]
_dev-all:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    (cd backend && air) &
    pnpm -C frontend dev

# Rebuild Docker dev environment
[group('dev')]
_dev-docker:
    ./scripts/development/dev.sh rebuild

# View Docker dev environment logs
[group('dev')]
_dev-logs:
    ./scripts/development/dev.sh logs

# Run development servers. Valid targets: "frontend", "backend", "all", "docker", "logs".
[group('dev')]
dev target="docker":
    @just "_dev-{{ target }}"

# Generate edge tunnel protobuf/gRPC code.
[group('proto')]
proto-backend:
    cd {{ edge_proto_dir }} && go run github.com/bufbuild/buf/cmd/buf@v1.65.0 generate

# Benchmark edge tunnel transport performance (gRPC vs WebSocket) with allocations.

# Usage: just bench-edge-tunnel [count] [benchtime]
[group('bench')]
bench-edge-tunnel count="3" benchtime="2s":
    cd backend && go test -run '^$' -bench '^BenchmarkEdgeTunnelProxyRequest$' -benchmem -count={{ count }} -benchtime={{ benchtime }} ./pkg/libarcane/edge

# Benchmark edge tunnel transport and write memory profile.

# Usage: just bench-edge-tunnel-mem [profile] [benchtime]
[group('bench')]
bench-edge-tunnel-mem profile="edge_tunnel.mem.out" benchtime="5s":
    cd backend && go test -run '^$' -bench '^BenchmarkEdgeTunnelProxyRequest$' -benchmem -benchtime={{ benchtime }} -memprofile={{ profile }} ./pkg/libarcane/edge

# Add a new i18n locale. Example:

# just i18n-add es "Español"
[group('i18n')]
i18n-add locale native_name settings="frontend/project.inlang/settings.json" picker="frontend/src/lib/components/locale-picker.svelte" messages_dir="frontend/messages" base_locale="en":
    #!/usr/bin/env bash
    set -euo pipefail

    if [ -z "{{ locale }}" ] || [ -z "{{ native_name }}" ]; then
        echo "Usage: just i18n-add <locale> <native_name> [settings] [picker] [messages_dir] [base_locale]"
        exit 1
    fi

    settings_path="{{ settings }}"
    picker_path="{{ picker }}"
    messages_dir="{{ messages_dir }}"
    base_locale="{{ base_locale }}"
    base_file="${messages_dir}/${base_locale}.json"
    target_file="${messages_dir}/{{ locale }}.json"

    if [ ! -f "$settings_path" ]; then
        echo "Settings file not found: $settings_path"
        exit 1
    fi

    if [ ! -f "$picker_path" ]; then
        echo "Locale picker file not found: $picker_path"
        exit 1
    fi

    if [ ! -f "$base_file" ]; then
        echo "Base messages file not found: $base_file"
        exit 1
    fi

    if ! command -v jq >/dev/null 2>&1; then
        echo "jq is required to update $settings_path"
        exit 1
    fi

    jq_tab="--tab"
    if ! jq --tab -n '{}' >/dev/null 2>&1; then
        jq_tab=""
    fi

    settings_tmp="$(mktemp)"
    jq $jq_tab --arg locale "{{ locale }}" \
        '.locales |= ( . + [$locale] | unique | sort_by(ascii_downcase) )' \
        "$settings_path" > "$settings_tmp"
    mv "$settings_tmp" "$settings_path"

    if ! command -v rg >/dev/null 2>&1; then
        echo "rg (ripgrep) is required to update $picker_path"
        exit 1
    fi

    start_line="$(rg -n -F "const locales: Record<string, string> = {" "$picker_path" | head -n1 | cut -d: -f1)"
    if [ -z "$start_line" ]; then
        echo "Unable to find locales map in $picker_path"
        exit 1
    fi

    end_line="$(awk -v s="$start_line" 'NR>=s && $0 ~ /^[[:space:]]*};/ { print NR; exit }' "$picker_path")"
    if [ -z "$end_line" ]; then
        echo "Unable to find end of locales map in $picker_path"
        exit 1
    fi

    const_indent="$(sed -n "${start_line}p" "$picker_path" | sed -E 's/^([[:space:]]*).*/\1/')"
    entry_indent="$(sed -n "$((start_line+1)),$((end_line-1))p" "$picker_path" | awk 'NF { match($0, /^[[:space:]]*/); print substr($0, RSTART, RLENGTH); exit }')"
    if [ -z "$entry_indent" ]; then
        entry_indent="${const_indent}\t"
    fi

    entries_tmp="$(mktemp)"
    while IFS= read -r line; do
        if [[ $line =~ ^[[:space:]]*\'?([^\'\":]+)\'?[[:space:]]*:[[:space:]]*\'(.*)\'[[:space:]]*,[[:space:]]*$ ]]; then
            key="${BASH_REMATCH[1]}"
            value="${BASH_REMATCH[2]}"
            if [ "$key" != "{{ locale }}" ]; then
                printf '%s\t%s\n' "$key" "$value" >> "$entries_tmp"
            fi
        fi
    done < <(sed -n "$((start_line+1)),$((end_line-1))p" "$picker_path")

    printf '%s\t%s\n' "{{ locale }}" "{{ native_name }}" >> "$entries_tmp"

    new_block="${const_indent}const locales: Record<string, string> = {"
    new_block+=$'\n'
    while IFS=$'\t' read -r key value; do
        [ -z "$key" ] && continue
        if [[ $key =~ ^[A-Za-z_$][A-Za-z0-9_$]*$ ]]; then
            out_key="$key"
        else
            esc_key="${key//\\/\\\\}"
            esc_key="${esc_key//\'/\\\'}"
            out_key="'${esc_key}'"
        fi
        esc_value="${value//\\/\\\\}"
        esc_value="${esc_value//\'/\\\'}"
        new_block+="${entry_indent}${out_key}: '${esc_value}',"
        new_block+=$'\n'
    done < <(LC_ALL=C sort -f -t $'\t' -k1,1 "$entries_tmp")
    new_block+="${const_indent}};"
    rm -f "$entries_tmp"

    block_tmp="$(mktemp)"
    printf '%s\n' "$new_block" > "$block_tmp"

    picker_tmp="$(mktemp)"
    sed -n "1,$((start_line-1))p" "$picker_path" > "$picker_tmp"
    cat "$block_tmp" >> "$picker_tmp"
    tail -n "+$((end_line+1))" "$picker_path" >> "$picker_tmp"
    mv "$picker_tmp" "$picker_path"
    rm -f "$block_tmp"

    if [ -f "$target_file" ]; then
        echo "Messages file already exists, not overwriting: $target_file"
    else
        cp "$base_file" "$target_file"
        echo "Created messages file: $target_file"
    fi

# --- Utils ---

[group('utils')]
_utils-list-fixes:
    #!/usr/bin/env bash
    set -euo pipefail

    TEST=false
    VERBOSE=false
    for arg in "$@"; do
        case "$arg" in
        --test)
            TEST=true
            ;;
        --verbose)
            VERBOSE=true
            ;;
        *)
            ;;
        esac
    done

    if [ "$VERBOSE" == true ]; then
        set -x
    fi

    # Colors for output
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color

    # Get the latest release tag
    LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

    if [ -z "$LATEST_TAG" ]; then
        echo -e "${YELLOW}No previous release tag found. Showing all fix commits:${NC}"
        RANGE="HEAD"
    else
        echo -e "${GREEN}Latest release: ${LATEST_TAG}${NC}"
        RANGE="${LATEST_TAG}..HEAD"
    fi

    echo ""
    echo -e "${BLUE}=== Fix commits on main branch since ${LATEST_TAG:-beginning} ===${NC}"
    echo ""

    # List all fix commits
    FIX_COMMITS=$(git log "$RANGE" \
        --oneline \
        --no-merges \
        --grep="^fix:" \
        --grep="^hotfix:" \
        --regexp-ignore-case \
        --pretty=format:"%C(yellow)%h%Creset %C(green)%ai%Creset %s %C(dim)(%an)%Creset" || echo "")

    if [ -z "$FIX_COMMITS" ]; then
        echo "No fix commits found."
    else
        echo "$FIX_COMMITS"
    fi

    echo ""
    echo ""
    echo -e "${BLUE}=== How to create a hotfix release ===${NC}"
    echo ""
    echo "1. Start the hotfix release process:"
    echo -e "   ${GREEN}just utils hotfix${NC}"
    echo ""
    echo "2. Cherry-pick the fixes you want:"
    echo -e "   ${GREEN}git cherry-pick <commit-hash>${NC}"
    echo ""
    echo "3. Finalize the release:"
    echo -e "   ${GREEN}just utils hotfix${NC}"
    echo ""

    if [ "$TEST" == true ]; then
        echo "Test mode: no changes were made."
    fi

[group('utils')]
_utils-hotfix:
    #!/usr/bin/env bash
    set -euo pipefail

    TEST=false
    VERBOSE=false
    for arg in "$@"; do
        case "$arg" in
        --test)
            TEST=true
            ;;
        --verbose)
            VERBOSE=true
            ;;
        *)
            ;;
        esac
    done

    CLIFF_VERBOSE=""
    if [ "$VERBOSE" == true ]; then
        set -x
        CLIFF_VERBOSE="-vv"
    fi

    # Colors for output
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color

    # Check if the script is being run from the root of the project
    if [ ! -f .arcane.json ] || [ ! -f frontend/package.json ] || [ ! -f CHANGELOG.md ]; then
        echo -e "${RED}Error: This command must be run from the root of the project.${NC}"
        exit 1
    fi

    CLIFF_CMD=""
    if command -v git-cliff &>/dev/null; then
        CLIFF_CMD="git-cliff"
    elif git cliff --version &>/dev/null; then
        CLIFF_CMD="git cliff"
    else
        echo "Error: git cliff is not installed. Please install it from https://git-cliff.org/docs/installation."
        exit 1
    fi

    # Check if GitHub CLI is installed
    if ! command -v gh &>/dev/null; then
        echo -e "${RED}Error: GitHub CLI (gh) is not installed. Please install it and authenticate using 'gh auth login'.${NC}"
        exit 1
    fi

    # Ensure local tags are up to date
    git fetch --tags --quiet || true

    # Get the latest repo-wide release tag (ignore module tags like cli/v* or types/v*)
    get_latest_repo_tag() {
        local ref="${1:-HEAD}"
        git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname --merged "$ref" | head -n1
    }

    LATEST_TAG=$(get_latest_repo_tag "main")
    if [ -z "$LATEST_TAG" ]; then
        LATEST_TAG=$(get_latest_repo_tag "HEAD")
    fi
    if [ -z "$LATEST_TAG" ]; then
        echo -e "${RED}Error: No previous release tag found.${NC}"
        exit 1
    fi

    echo -e "${GREEN}Latest release tag: ${LATEST_TAG}${NC}"

    # Extract version components from tag (format: v1.2.3)
    VERSION=${LATEST_TAG#v}
    IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"

    # Increment patch version for hotfix
    NEW_PATCH=$((PATCH + 1))
    NEW_VERSION="${MAJOR}.${MINOR}.${NEW_PATCH}"
    NEW_TAG="v${NEW_VERSION}"

    echo -e "${YELLOW}Proposed hotfix version: ${NEW_TAG}${NC}"

    # Create release branch name
    RELEASE_BRANCH="release/v${MAJOR}.${MINOR}"

    echo ""
    echo -e "${YELLOW}=== Available fix commits since ${LATEST_TAG} ===${NC}"
    echo ""

    # List all fix commits since the last tag
    FIX_COMMITS=$(git log "${LATEST_TAG}..main" \
        --oneline \
        --no-merges \
        --grep="^fix:" \
        --grep="^hotfix:" \
        --regexp-ignore-case \
        --pretty=format:"%C(yellow)%h%Creset %s %C(dim)(%an)%Creset" || echo "")

    if [ -z "$FIX_COMMITS" ]; then
        echo -e "${RED}No fix commits found since ${LATEST_TAG}${NC}"
    else
        echo "$FIX_COMMITS"
    fi

    if [ "$TEST" == true ]; then
        echo ""
        echo "Test mode enabled: no changes will be made."
        echo "Would create or reuse release branch: ${RELEASE_BRANCH}"
        echo "Would cherry-pick selected commits from main."
        echo "Would generate changelog for ${NEW_TAG} and tag release."
        echo "Would update main to version ${NEW_VERSION} after release."
        exit 0
    fi

    # Check if release branch already exists
    if git show-ref --verify --quiet "refs/heads/${RELEASE_BRANCH}"; then
        echo -e "${YELLOW}Release branch ${RELEASE_BRANCH} already exists.${NC}"
        read -p "Do you want to use the existing branch? (y/n) " USE_EXISTING
        if [[ "$USE_EXISTING" != "y" ]]; then
            echo -e "${RED}Aborting.${NC}"
            exit 1
        fi
        git checkout "${RELEASE_BRANCH}"
    else
        echo -e "${BLUE}Creating new release branch from ${LATEST_TAG}...${NC}"
        git checkout -b "${RELEASE_BRANCH}" "${LATEST_TAG}"
    fi

    if [ -z "$FIX_COMMITS" ]; then
        echo ""
        read -p "Do you want to continue anyway? (y/n) " CONTINUE
        if [[ "$CONTINUE" != "y" ]]; then
            git checkout main
            git branch -D "${RELEASE_BRANCH}" 2>/dev/null || true
            exit 0
        fi
    else
        echo ""
    fi

    echo -e "${BLUE}=== Cherry-pick Instructions ===${NC}"
    echo ""
    echo "You can now cherry-pick the commits you want to include in this hotfix."
    echo ""
    echo "Example:"
    echo -e "  ${GREEN}git cherry-pick abc123 def456${NC}"
    echo ""
    echo "Or pick them one at a time and continue when done."
    echo ""

    # Interactive cherry-pick loop
    while true; do
        echo ""
        read -p "Enter commit hash to cherry-pick (or 'done' to finish, 'list' to see commits again, 'quit' to abort): " INPUT

        case "$INPUT" in
            done)
                break
                ;;
            quit)
                echo -e "${YELLOW}Aborting hotfix release...${NC}"
                git checkout main
                read -p "Delete release branch ${RELEASE_BRANCH}? (y/n) " DELETE_BRANCH
                if [[ "$DELETE_BRANCH" == "y" ]]; then
                    git branch -D "${RELEASE_BRANCH}" 2>/dev/null || true
                fi
                exit 0
                ;;
            list)
                echo ""
                echo -e "${YELLOW}=== Available fix commits ===${NC}"
                echo "$FIX_COMMITS"
                continue
                ;;
            "")
                continue
                ;;
            *)
                # Try to cherry-pick the commit(s)
                if git cherry-pick $INPUT; then
                    echo -e "${GREEN}✓ Successfully cherry-picked: $INPUT${NC}"
                else
                    echo -e "${RED}✗ Cherry-pick failed. Resolve conflicts and run:${NC}"
                    echo -e "  ${YELLOW}git add <resolved-files>${NC}"
                    echo -e "  ${YELLOW}git cherry-pick --continue${NC}"
                    echo ""
                    read -p "Press Enter when resolved (or 'abort' to skip this commit): " RESOLVE
                    if [[ "$RESOLVE" == "abort" ]]; then
                        git cherry-pick --abort
                        echo -e "${YELLOW}Cherry-pick aborted${NC}"
                    fi
                fi
                ;;
        esac
    done

    # Check if any commits were added
    BASE_TAG=$(get_latest_repo_tag "HEAD")
    if [ -z "$BASE_TAG" ]; then
        BASE_TAG="$LATEST_TAG"
    fi
    COMMITS_ADDED=$(git rev-list "${BASE_TAG}..HEAD" --count)

    if [ "$COMMITS_ADDED" -eq 0 ]; then
        echo -e "${RED}No commits were cherry-picked. Aborting hotfix release.${NC}"
        git checkout main
        git branch -D "${RELEASE_BRANCH}" 2>/dev/null || true
        exit 0
    fi

    echo ""
    echo -e "${YELLOW}=== Commits in this hotfix release ===${NC}"
    git log "${BASE_TAG}..HEAD" --oneline --no-merges
    echo ""

    # Confirm release
    read -p "Create hotfix release ${NEW_TAG}? (y/n) " CONFIRM
    if [[ "$CONFIRM" != "y" ]]; then
        echo -e "${RED}Release canceled.${NC}"
        echo ""
        echo "You are still on branch ${RELEASE_BRANCH}."
        echo "To continue later, run this command again or finalize manually."
        exit 1
    fi

    echo ""
    echo -e "${BLUE}Finalizing hotfix release ${NEW_TAG}...${NC}"

    # Update .arcane.json file with the new version and revision
    LATEST_REVISION=$(git rev-parse HEAD)
    jq --arg version "$NEW_VERSION" --arg revision "$LATEST_REVISION" \
      '.version = $version | .revision = $revision' .arcane.json > .arcane_tmp.json && mv .arcane_tmp.json .arcane.json
    git add .arcane.json

    # Update version in frontend/package.json
    jq --arg new_version "$NEW_VERSION" '.version = $new_version' frontend/package.json > frontend/package_tmp.json && mv frontend/package_tmp.json frontend/package.json
    git add frontend/package.json

    # Generate changelog for ONLY the fixes in this release branch
    echo -e "${BLUE}Generating changelog for hotfix...${NC}"

    $CLIFF_CMD $CLIFF_VERBOSE \
        --github-token=$(gh auth token) \
        --prepend CHANGELOG.md \
        --tag "$NEW_TAG" \
        --unreleased

    git add CHANGELOG.md

    # Commit the version bump and changelog
    git commit \
        -m "release: ${NEW_VERSION} (hotfix)" \
        -m "Hotfix release containing critical bug fixes." \
        -m "Base version: ${BASE_TAG}" \
        -m "See CHANGELOG.md for details."

    # Create annotated tag
    git tag -a "$NEW_TAG" \
        -m "Release ${NEW_TAG} (Hotfix)" \
        -m "Hotfix release based on ${BASE_TAG}" \
        -m "See CHANGELOG.md for details."

    git tag "cli/${NEW_TAG}"
    git tag "types/${NEW_TAG}"
    git tag "backend/${NEW_TAG}"

    echo ""
    echo -e "${GREEN}✅ Hotfix release ${NEW_TAG} created successfully!${NC}"
    echo ""

    # Push the commit and tags in two steps to ensure the release workflow triggers on v* tag push
    git push
    git push origin "$NEW_TAG"
    git push origin "cli/${NEW_TAG}" "types/${NEW_TAG}" "backend/${NEW_TAG}"

    # Extract the changelog content for the latest release
    echo "Extracting changelog content for version $NEW_TAG..."
    CHANGELOG=$(awk '/^## v[0-9]/ { if (found) exit; found=1; next } found' CHANGELOG.md)

    if [ -z "$CHANGELOG" ]; then
        echo -e "${RED}Error: Could not extract changelog for version $NEW_TAG.${NC}"
        exit 1
    fi

    # Create the draft release on GitHub
    echo "Creating GitHub draft release..."
    gh release create "$NEW_TAG" --title "${NEW_TAG} (Hotfix)" --notes "$CHANGELOG" --draft

    if [ $? -eq 0 ]; then
        echo "GitHub draft release created successfully."
    else
        echo -e "${RED}Error: Failed to create GitHub release.${NC}"
        exit 1
    fi

    echo "Release process complete. New version: $NEW_TAG"
    echo ""
    echo -e "${YELLOW}Note: Hotfix release created from ${BASE_TAG} on ${RELEASE_BRANCH}${NC}"
    echo "The fix commits are already on main, this release just tags them without unreleased features."
    echo ""
    echo "Review the draft at: https://github.com/ofkm/arcane/releases"
    echo ""

    # Update main branch with the new version files
    echo -e "${BLUE}Updating main branch with version ${NEW_VERSION}...${NC}"
    git checkout main
    git pull origin main --quiet

    # Update .arcane.json file with the new version and revision
    jq --arg version "$NEW_VERSION" --arg revision "$LATEST_REVISION" \
      '.version = $version | .revision = $revision' .arcane.json > .arcane_tmp.json && mv .arcane_tmp.json .arcane.json

    # Update version in frontend/package.json
    jq --arg new_version "$NEW_VERSION" '.version = $new_version' frontend/package.json > frontend/package_tmp.json && mv frontend/package_tmp.json frontend/package.json

    # Copy the updated CHANGELOG.md from the release branch
    git checkout "${RELEASE_BRANCH}" -- CHANGELOG.md

    # Commit the version updates to main
    git add .arcane.json frontend/package.json CHANGELOG.md
    git commit -m "chore: bump version to ${NEW_VERSION} after hotfix release"
    git push origin main

    echo -e "${GREEN}✅ Main branch updated with version ${NEW_VERSION}${NC}"
    echo ""

# Utils targets. Valid: "list-fixes", "hotfix".
[group('utils')]
utils target *args:
    @just "_utils-{{ target }}" {{ args }}

# Build the frontend
[group('build')]
_build-frontend:
    pnpm -C frontend build

# Build the backend
[group('build')]
_build-backend:
    cd backend && go build ./...

# Build manager container image
[group('build')]
_build-image-manager tag="arcane:latest" flag='':
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64 -f 'docker/Dockerfile' --build-arg ENABLED_FEATURES="{{ env('ENABLED_FEATURES', env('BUILD_FEATURES', '')) }}" -t "{{ tag }}" .

# Build agent container image
[group('build')]
_build-image-agent tag="arcane-agent:latest" flag='':
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64 -f 'docker/Dockerfile-agent' --build-arg ENABLED_FEATURES="{{ env('ENABLED_FEATURES', env('BUILD_FEATURES', '')) }}" -t "{{ tag }}" .

# Build both frontend and backend
[group('build')]
_build-all:
    @just _build-frontend
    @just _build-backend

# Build targets. Valid: "single frontend", "single backend", "single all", "image manager [tag] [--push]", "image agent [tag] [--push]"
[group('build')]
build buildtype type tag="" flag="":
    @if [ "{{ buildtype }}" = "single" ]; then just _build-{{ type }}; elif [ "{{ buildtype }}" = "image" ]; then just _build-image-{{ type }} "{{ if tag != "" { tag } else if type == "manager" { "arcane:latest" } else { "arcane-agent:latest" } }}" "{{ flag }}"; fi

# --- Release ---

# Create a new release (use --test to dry-run without writing anything)
[group('release')]
release *args:
    #!/usr/bin/env bash
    set -euo pipefail

    TEST=false
    FORCE_MAJOR=false
    VERBOSE=false
    set -- {{ args }}
    for arg in "$@"; do
        case "$arg" in
        --test)
            TEST=true
            ;;
        --major)
            FORCE_MAJOR=true
            ;;
        --verbose)
            VERBOSE=true
            ;;
        *)
            ;;
        esac
    done

    CLIFF_VERBOSE=""
    if [ "$VERBOSE" == true ]; then
        CLIFF_VERBOSE="-vv"
    fi

    # Check if the script is being run from the root of the project
    if [ ! -f .arcane.json ] || [ ! -f frontend/package.json ] || [ ! -f CHANGELOG.md ]; then
        echo "Error: This command must be run from the root of the project."
        exit 1
    fi

    # Check if git cliff is installed
    if ! command -v git cliff &>/dev/null; then
        echo "Error: git cliff is not installed. Please install it from https://git-cliff.org/docs/installation."
        exit 1
    fi

    # Check if GitHub CLI is installed
    if ! command -v gh &>/dev/null; then
        echo "Error: GitHub CLI (gh) is not installed. Please install it and authenticate using 'gh auth login'."
        exit 1
    fi

    # Ensure local tags are up to date (don't fail if no remote)
    git fetch --tags --quiet || true

    # Check if we're on the main branch (skip in test mode)
    if [ "$TEST" == false ] && [ "$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then
        echo "Error: This command must be run on the main branch."
        exit 1
    fi

    # Read the current version from .arcane.json
    VERSION=$(jq -r '.version' .arcane.json)

    # Function to increment the version
    increment_version() {
        local version=$1
        local part=$2

        IFS='.' read -r -a parts <<<"$version"
        if [ "$part" == "major" ]; then
            parts[0]=$((parts[0] + 1))
            parts[1]=0
            parts[2]=0
        elif [ "$part" == "minor" ]; then
            parts[1]=$((parts[1] + 1))
            parts[2]=0
        elif [ "$part" == "patch" ]; then
            parts[2]=$((parts[2] + 1))
        fi
        echo "${parts[0]}.${parts[1]}.${parts[2]}"
    }

    # Determine the release type
    if [ "$FORCE_MAJOR" == true ]; then
        RELEASE_TYPE="major"
    else
        # Get the latest version tag (v*), ignoring non-version tags like 'next'
        LATEST_TAG=$(git tag -l 'v*' --sort=-v:refname | head -n1 || echo "")
        if [ -z "$LATEST_TAG" ]; then
            RELEASE_TYPE="minor"
        else
            # Look only at commit subjects since the last tag (exclude merges)
            SUBJECTS=$(git log --no-merges --format=%s "${LATEST_TAG}..HEAD")

            if echo "$SUBJECTS" | grep -Eiq '^feat(\([^)]+\))?: '; then
                RELEASE_TYPE="minor"
            elif echo "$SUBJECTS" | grep -Eiq '^fix(\([^)]+\))?: '; then
                RELEASE_TYPE="patch"
            else
                echo "No 'fix' or 'feat' commits found since the latest release (${LATEST_TAG}). No new release will be created."
                echo "Commits since ${LATEST_TAG}:"
                git log --oneline --no-merges "${LATEST_TAG}..HEAD" || true
                exit 0
            fi
        fi
    fi

    # Increment the version based on the release type
    if [ "$RELEASE_TYPE" == "major" ]; then
        echo "Performing major release..."
        NEW_VERSION=$(increment_version "$VERSION" major)
    elif [ "$RELEASE_TYPE" == "minor" ]; then
        echo "Performing minor release..."
        NEW_VERSION=$(increment_version "$VERSION" minor)
    elif [ "$RELEASE_TYPE" == "patch" ]; then
        echo "Performing patch release..."
        NEW_VERSION=$(increment_version "$VERSION" patch)
    else
        echo "Invalid release type. Please enter either 'major', 'minor', or 'patch'."
        exit 1
    fi

    if [ "$TEST" == true ]; then
        echo "Test mode enabled: no files will be modified, no commits/tags/pushes/releases will be created."
    else
        # Confirm release creation
        read -p "This will create a new $RELEASE_TYPE release with version $NEW_VERSION. Do you want to proceed? (y/n) " CONFIRM
        if [[ "$CONFIRM" != "y" ]]; then
            echo "Release process canceled."
            exit 1
        fi
    fi

    LATEST_REVISION=$(git rev-parse HEAD)

    if [ "$TEST" == false ]; then
        # Update .arcane.json file with the new version and revision
        echo "Updating .arcane.json file..."
        jq --arg version "$NEW_VERSION" --arg revision "$LATEST_REVISION" \
          '.version = $version | .revision = $revision' .arcane.json > .arcane_tmp.json && mv .arcane_tmp.json .arcane.json
        git add .arcane.json

        # Update version in frontend/package.json
        jq --arg new_version "$NEW_VERSION" '.version = $new_version' frontend/package.json > frontend/package_tmp.json && mv frontend/package_tmp.json frontend/package.json
        git add frontend/package.json

        # Generate changelog
        echo "Generating changelog..."
        git cliff $CLIFF_VERBOSE --github-token=$(gh auth token) --prepend CHANGELOG.md --tag "v$NEW_VERSION" --unreleased
        git add CHANGELOG.md

        # Commit the changes with the new version
        git commit -m "release: $NEW_VERSION"

        # Create Git tags with the new version
        TAG_MESSAGE="$NEW_VERSION"
        git tag -a "v$NEW_VERSION" -m "$TAG_MESSAGE"
        git tag -a "cli/v$NEW_VERSION" -m "$TAG_MESSAGE"
        git tag -a "types/v$NEW_VERSION" -m "$TAG_MESSAGE"
        git tag -a "backend/v$NEW_VERSION" -m "$TAG_MESSAGE"

        # Push the commit and tags in two steps to ensure the release workflow triggers on v* tag push
        git push
        git push origin "v$NEW_VERSION"
        git push origin "cli/v$NEW_VERSION" "types/v$NEW_VERSION" "backend/v$NEW_VERSION"

        # Extract the changelog content for the latest release
        echo "Extracting changelog content for version $NEW_VERSION..."
        CHANGELOG=$(awk '/^## v[0-9]/ { if (found) exit; found=1; next } found' CHANGELOG.md)

        if [ -z "$CHANGELOG" ]; then
            echo "Error: Could not extract changelog for version $NEW_VERSION."
            exit 1
        fi

        # Create the draft release on GitHub
        echo "Creating GitHub draft release..."
        gh release create "v$NEW_VERSION" --title "v$NEW_VERSION" --notes "$CHANGELOG" --draft

        if [ $? -eq 0 ]; then
            echo "GitHub draft release created successfully."
        else
            echo "Error: Failed to create GitHub release."
            exit 1
        fi

        echo "Release process complete. New version: $NEW_VERSION"
    else
        echo "Test mode: skipping confirmation prompt and all write operations."
        echo "Would update .arcane.json to version $NEW_VERSION and revision $LATEST_REVISION"
        echo "Would update frontend/package.json to version $NEW_VERSION"
        echo "Generating changelog preview (no file write)..."
        CHANGELOG=$(git cliff $CLIFF_VERBOSE --github-token=$(gh auth token) --tag "v$NEW_VERSION" --unreleased)

        if [ -z "$CHANGELOG" ]; then
            echo "Error: Could not generate changelog preview for version $NEW_VERSION."
            exit 1
        fi

        echo "----- BEGIN CHANGELOG PREVIEW -----"
        echo "$CHANGELOG"
        echo "----- END CHANGELOG PREVIEW -----"
        echo "Would commit: release: $NEW_VERSION"
        echo "Would tag: v$NEW_VERSION, cli/v$NEW_VERSION, types/v$NEW_VERSION, backend/v$NEW_VERSION"
        echo "Would push commit"
        echo "Would push tag: v$NEW_VERSION"
        echo "Would push tags: cli/v$NEW_VERSION, types/v$NEW_VERSION, backend/v$NEW_VERSION"
        echo "Would create GitHub draft release v$NEW_VERSION"
        echo "Test mode complete. No changes were written."
    fi

# --- Testing ---

# Run Playwright E2E tests
[group('tests')]
_test-e2e:
    pnpm -C tests test

# Run backend Go tests
[group('tests')]
_test-backend:
    cd backend && go test -tags=exclude_frontend,buildables -ldflags "-X github.com/getarcaneapp/arcane/backend/buildables.EnabledFeatures=autologin" ./... -race -coverprofile=coverage.txt -covermode=atomic -v

# Run CLI tests
[group('tests')]
_test-cli:
    cd cli && go test ./... -race -coverprofile=coverage.txt -covermode=atomic -v

[group('tests')]
_test-all:
    @just _test-e2e
    @just _test-backend
    @just _test-cli

# Run tests. Valid targets: "e2e", "backend", "cli", "all".
[group('tests')]
test target="all":
    @just "_test-{{ target }}"

# --- Dependency Management ---

# Update frontend dependencies
[group('deps')]
_deps-update-frontend:
    pnpm update

# Update backend Go dependencies
[group('deps')]
_deps-update-backend:
    cd backend && go get -u ./... && go mod tidy

# Update pnpm version via corepack
[group('deps')]
_deps-update-pnpm:
    npx corepack up

[group('deps')]
_deps-update-all: _deps-update-frontend _deps-update-backend _deps-update-pnpm

# Install frontend dependencies
[group('deps')]
_deps-install-frontend:
    pnpm install

# Install tests dependencies
[group('deps')]
_deps-install-tests:
    pnpm -C tests install
    pnpm -C tests exec playwright install --with-deps chromium

# Install backend Go dependencies
[group('deps')]
_deps-install-backend:
    cd backend && go mod download
    go work sync

# Install CLI Go dependencies
[group('deps')]
_deps-install-cli:
    cd cli && go mod download
    go work sync

# Install types Go dependencies
[group('deps')]
_deps-install-types:
    cd types && go mod download
    go work sync

# Install all Go dependencies
[group('deps')]
_deps-install-go: _deps-install-backend _deps-install-cli _deps-install-types

# Install all Node.js dependencies
[group('deps')]
_deps-install-node: _deps-install-frontend _deps-install-tests

# Install all dependencies
[group('deps')]
_deps-install-all: _deps-install-node _deps-install-go

# Deps targets. Valid: "install [frontend|tests|backend|cli|types|go|node|all]", "update [frontend|backend|pnpm|all]"
[group('deps')]
deps action="update" target="all":
    @just "_deps-{{ action }}-{{ target }}"

# Type check/Lint frontend
[group('lint')]
_lint-frontend:
    pnpm -C frontend check

[group('lint')]
_lint-all:
    @just _lint-frontend
    @just _lint-go

# Lint Go backend
[group('lint')]
_lint-backend:
    cd backend && golangci-lint run -c ../.github/.golangci.yml ./...

# Lint Go CLI
[group('lint')]
_lint-cli:
    cd cli && golangci-lint run -c ../.github/.golangci.yml ./...

# Lint Types
[group('lint')]
_lint-types:
    cd types && golangci-lint run -c ../.github/.golangci.yml ./...

# Lint all Go code
[group('lint')]
_lint-go: _lint-backend _lint-cli _lint-types

# Lint targets. Valid: "backend", "frontend", "cli", "types" "all".
[group('lint')]
lint target="all":
    @just "_lint-{{ target }}"

# Format frontend (Prettier) and Go modules (gofmt)
[group('format')]
_format-frontend:
    pnpm -C frontend format

[group('format')]
_format-go:
    cd backend && gofmt -s -w .
    cd cli && gofmt -s -w .
    cd types && gofmt -s -w .

[group('format')]
_format-just:
    just --fmt --unstable

[group('format')]
_format-check-frontend:
    pnpm -C frontend format:check

[group('format')]
_format-check-go:
    #!/usr/bin/env bash
    set -euo pipefail

    unformatted=$(gofmt -l backend cli types)
    if [ -n "$unformatted" ]; then
        echo "Unformatted Go files:"
        echo "$unformatted"
        exit 1
    fi

[group('format')]
_format-all:
    @just _format-frontend
    @just _format-go
    @just _format-just

[group('format')]
_format-check-all:
    @just _format-check-frontend
    @just _format-check-go

# Format targets. Valid: "frontend", "go", "just", "all". Use --check to verify formatting.
[group('format')]
format target="all" check="":
    @if [ "{{ check }}" = "--check" ]; then just "_format-check-{{ target }}"; else just "_format-{{ target }}"; fi

# Clean build artifacts
[group('repo')]
_repo-clean:
    rm -rf frontend/.svelte-kit frontend/build backend/.bin
    find . -type d -name node_modules -prune -exec rm -rf {} \;

# Repo targets. Valid: "clean".
[group('repo')]
repo target="clean":
    @just "_repo-{{ target }}"
