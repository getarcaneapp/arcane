# -----------------------------------------------------------------------------
# Project settings
# -----------------------------------------------------------------------------

set working-directory := './'

edge_proto_dir := 'backend/proto'

_default:
    @just --list

# -----------------------------------------------------------------------------
# Development
# -----------------------------------------------------------------------------

# Run frontend dev server on port 3000
[group('dev')]
_dev-frontend:
    vp -C frontend run dev

# Run backend with hot reload on port 3552
[group('dev')]
_dev-backend:
    cd backend && air

[group('dev')]
_dev-agent:
    #!/usr/bin/env bash
    set -euo pipefail

    if [ -z "${AGENT_TOKEN:-}" ]; then
        echo "AGENT_TOKEN is required. Run: AGENT_TOKEN=<edge-environment-token> just dev agent"
        exit 1
    fi

    port="${PORT:-3553}"
    app_url="${APP_URL:-http://localhost:${port}}"
    manager_api_url="${MANAGER_API_URL:-https://localhost:3552}"
    edge_mtls_assets_dir="${EDGE_MTLS_ASSETS_DIR:-./.tmp/edge-test-agent/edge-mtls-agent}"
    edge_mtls_ca_file="${EDGE_MTLS_CA_FILE:-./backend/local-manager.crt}"
    database_url="${DATABASE_URL:-file:./.tmp/edge-test-agent/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2500)&_txlock=immediate}"
    projects_directory="${PROJECTS_DIRECTORY:-./.tmp/edge-test-agent/projects}"
    git_work_dir="${GIT_WORK_DIR:-./.tmp/edge-test-agent/git}"
    jwt_secret="${JWT_SECRET:-local-edge-test-jwt-secret-please-change}"
    encryption_key="${ENCRYPTION_KEY:-local-edge-test-encryption-key-32}"

    mkdir -p "${projects_directory}" "${git_work_dir}" "${edge_mtls_assets_dir}"

    PORT="${port}" \
    APP_URL="${app_url}" \
    EDGE_AGENT=true \
    EDGE_TRANSPORT=poll \
    EDGE_MTLS_MODE=required \
    EDGE_MTLS_ASSETS_DIR="${edge_mtls_assets_dir}" \
    EDGE_MTLS_CA_FILE="${edge_mtls_ca_file}" \
    AGENT_TOKEN="${AGENT_TOKEN}" \
    MANAGER_API_URL="${manager_api_url}" \
    DATABASE_URL="${database_url}" \
    PROJECTS_DIRECTORY="${projects_directory}" \
    GIT_WORK_DIR="${git_work_dir}" \
    JWT_SECRET="${jwt_secret}" \
    ENCRYPTION_KEY="${encryption_key}" \
    go run ./backend/cmd

[group('dev')]
_dev-all:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    (cd backend && air) &
    vp -C frontend run dev

# Rebuild Docker dev environment
[group('dev')]
_dev-docker:
    ./scripts/development/dev.sh rebuild

# View Docker dev environment logs
[group('dev')]
_dev-logs:
    ./scripts/development/dev.sh logs

# Run development servers. Valid targets: "frontend", "backend", "agent", "all", "docker", "logs".
[group('dev')]
dev target="docker":
    @just "_dev-{{ target }}"

# Generate a self-signed TLS cert + key for the local manager (used by the
# backend HTTPS listener on :3552 and pinned as EDGE_MTLS_CA_FILE by the
# local edge agent). Writes to backend/local-manager.{crt,key} with SANs for
# localhost + 127.0.0.1. Both files are gitignored via *.crt / *.key.
#
# Usage:
#   just dev-tls                  # generate if missing

# just dev-tls force=true       # overwrite existing files
[group('dev')]
dev-tls force="false":
    #!/usr/bin/env bash
    set -euo pipefail

    cert_path="./backend/local-manager.crt"
    key_path="./backend/local-manager.key"

    if [ "{{ force }}" != "true" ] && [ -f "${cert_path}" ] && [ -f "${key_path}" ]; then
        echo "Cert already exists at ${cert_path}; pass force=true to regenerate."
        exit 0
    fi

    go run ./cli generate tls \
        --out-dir ./backend \
        --cert-name "$(basename "${cert_path}")" \
        --key-name "$(basename "${key_path}")" \
        --common-name arcane-local-manager \
        --host localhost \
        --host arcane-local \
        --host 127.0.0.1 \
        --host ::1

    echo ""
    echo "Generated self-signed TLS cert:"
    echo "  cert: ${cert_path}"
    echo "  key:  ${key_path}"
    echo ""
    echo "Run the backend with HTTPS enabled:"
    echo "  TLS_ENABLED=true TLS_CERT_FILE=${cert_path} TLS_KEY_FILE=${key_path} just dev backend"
    echo ""
    echo "The local edge agent recipe already pins this cert via EDGE_MTLS_CA_FILE."

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------

# Build the frontend
[group('build')]
_build-frontend:
    vp -C frontend run build

# Build the backend
[group('build')]
_build-backend:
    cd backend && go build ./...

# Build both frontend and backend
[group('build')]
_build-all:
    @just _build-frontend
    @just _build-backend

# Build manager container image
[group('build')]
_build-image-manager tag="ghcr.io/getarcaneapp/arcane:development" flag='':
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64,linux/arm/v7 -f 'docker/Dockerfile' --build-arg ENABLED_FEATURES="{{ env('ENABLED_FEATURES', env('BUILD_FEATURES', '')) }}" -t "{{ tag }}" .

# Build agent container image
[group('build')]
_build-image-agent tag="ghcr.io/getarcaneapp/agent:development" flag='':
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64,linux/arm/v7 -f 'docker/Dockerfile-agent' --build-arg ENABLED_FEATURES="{{ env('ENABLED_FEATURES', env('BUILD_FEATURES', '')) }}" -t "{{ tag }}" .

# Build targets:
#   just build single {frontend|backend|all}
# just build image {manager|agent} [tag] [--push]
[group('build')]
build buildtype type="" tag="" flag="":
    @if [ "{{ buildtype }}" = "single" ]; then just _build-{{ type }}; \
    elif [ "{{ buildtype }}" = "image" ]; then just _build-image-{{ type }} "{{ if tag != "" { tag } else if type == "manager" { "arcane:latest" } else { "arcane-agent:latest" } }}" "{{ flag }}"; \
    else echo "Unknown build target: {{ buildtype }}. Try: just build single|image" >&2; exit 1; \
    fi

# -----------------------------------------------------------------------------
# Test
# -----------------------------------------------------------------------------

# Run Playwright E2E tests
[group('test')]
_test-e2e:
    vp -C tests run test

# Run backend Go tests
[group('test')]
_test-backend:
    #!/usr/bin/env bash
    set -euo pipefail

    cd backend
    if [ -n "${GO_JUNIT_REPORT_FILE:-}" ]; then
        mkdir -p "$(dirname "$GO_JUNIT_REPORT_FILE")"
        go test -json -tags=exclude_frontend,buildables -ldflags "-X github.com/getarcaneapp/arcane/backend/v2/buildables.EnabledFeatures=autologin" ./... -race -coverprofile=coverage.txt -covermode=atomic -v 2>&1 | go run github.com/jstemmer/go-junit-report/v2@v2.1.0 -parser gojson -set-exit-code -out "$GO_JUNIT_REPORT_FILE"
    else
        go test -tags=exclude_frontend,buildables -ldflags "-X github.com/getarcaneapp/arcane/backend/v2/buildables.EnabledFeatures=autologin" ./... -race -coverprofile=coverage.txt -covermode=atomic -v
    fi

# Run CLI tests
[group('test')]
_test-cli:
    #!/usr/bin/env bash
    set -euo pipefail

    cd cli
    if [ -n "${GO_JUNIT_REPORT_FILE:-}" ]; then
        mkdir -p "$(dirname "$GO_JUNIT_REPORT_FILE")"
        go test -json ./... -race -coverprofile=coverage.txt -covermode=atomic -v 2>&1 | go run github.com/jstemmer/go-junit-report/v2@v2.1.0 -parser gojson -set-exit-code -out "$GO_JUNIT_REPORT_FILE"
    else
        go test ./... -race -coverprofile=coverage.txt -covermode=atomic -v
    fi

# Run shared types Go tests
[group('test')]
_test-types:
    #!/usr/bin/env bash
    set -euo pipefail

    cd types
    if [ -n "${GO_JUNIT_REPORT_FILE:-}" ]; then
        mkdir -p "$(dirname "$GO_JUNIT_REPORT_FILE")"
        go test -json ./... -race -coverprofile=coverage.txt -covermode=atomic -v 2>&1 | go run github.com/jstemmer/go-junit-report/v2@v2.1.0 -parser gojson -set-exit-code -out "$GO_JUNIT_REPORT_FILE"
    else
        go test ./... -race -coverprofile=coverage.txt -covermode=atomic -v
    fi

[group('test')]
_test-all:
    @just _test-e2e
    @just _test-backend
    @just _test-cli
    @just _test-types

# Run tests. Valid targets: "e2e", "backend", "cli", "types", "all".
[group('test')]
test target="all":
    @just "_test-{{ target }}"

# -----------------------------------------------------------------------------
# Quality: format, lint, and fixes
# -----------------------------------------------------------------------------

# Format frontend/test/email TypeScript with vp fmt (Vite+) and Go modules with gofmt
[group('quality')]
_format-frontend:
    vp fmt frontend

[group('quality')]
_format-js:
    vp fmt tests
    vp fmt email-templates

[group('quality')]
_format-go:
    cd backend && gofmt -s -w .
    cd cli && gofmt -s -w .
    cd types && gofmt -s -w .

[group('quality')]
_format-just:
    just --fmt --unstable

[group('quality')]
_format-check-frontend:
    vp fmt --check frontend

[group('quality')]
_format-check-js:
    vp fmt --check tests
    vp fmt --check email-templates

[group('quality')]
_format-check-go:
    #!/usr/bin/env bash
    set -euo pipefail

    unformatted=$(gofmt -l backend cli types)
    if [ -n "$unformatted" ]; then
        echo "Unformatted Go files:"
        echo "$unformatted"
        exit 1
    fi

[group('quality')]
_format-all:
    #!/usr/bin/env bash
    # Run every formatter even if one fails, so e.g. a vp/pnpm hiccup can't skip gofmt
    failed=0
    for target in frontend js go just; do
        just "_format-${target}" || failed=1
    done
    exit "${failed}"

[group('quality')]
_format-check-all:
    @just _format-check-frontend
    @just _format-check-js
    @just _format-check-go

# Format targets. Valid: "frontend", "js", "go", "just", "all". Use --check to verify formatting.
[group('quality')]
format target="all" check="":
    @if [ "{{ check }}" = "--check" ]; then just "_format-check-{{ target }}"; else just "_format-{{ target }}"; fi

# Type check/Lint frontend
[group('quality')]
_lint-frontend:
    vp -C frontend run check

# Type check Playwright tests
[group('quality')]
_lint-tests:
    vp -C tests run check

# Type check email templates
[group('quality')]
_lint-email-templates:
    vp -C email-templates run check

# Type check all JavaScript/TypeScript workspaces
[group('quality')]
_lint-js:
    @just _lint-frontend
    @just _lint-tests
    @just _lint-email-templates

# Build golangci-lint with the custom linters enabled by the shared config
[group('quality')]
_build-golangci-lint:
    golangci-lint custom

# Lint Go backend
[group('quality')]
_lint-backend: _build-golangci-lint
    cd backend && ../.bin/golangci-lint-custom run -c ../.github/.golangci.yml ./...

# Lint Go CLI
[group('quality')]
_lint-cli: _build-golangci-lint
    cd cli && ../.bin/golangci-lint-custom run -c ../.github/.golangci.yml ./...

# Lint Types
[group('quality')]
_lint-types: _build-golangci-lint
    cd types && ../.bin/golangci-lint-custom run -c ../.github/.golangci.yml ./...

# Lint edge tunnel protobuf definitions.
[group('quality')]
_lint-proto:
    cd {{ edge_proto_dir }} && go run github.com/bufbuild/buf/cmd/buf@latest lint

# Lint all Go code
[group('quality')]
_lint-go: _lint-backend _lint-cli _lint-types

[group('quality')]
_lint-all:
    @just _lint-js
    @just _lint-go
    @just _lint-proto

# Lint targets. Valid: "backend", "frontend", "tests", "email-templates", "js", "cli", "types", "go", "proto", "all".
[group('quality')]
lint target="all":
    @just "_lint-{{ target }}"

# Fix Go backend
[group('quality')]
_fix-backend:
    cd backend && go fix ./...

# Fix Go CLI
[group('quality')]
_fix-cli:
    cd cli && go fix ./...

# Fix Types
[group('quality')]
_fix-types:
    cd types && go fix ./...

# Fix all Go code
[group('quality')]
_fix-go: _fix-backend _fix-cli _fix-types

[group('quality')]
_fix-all:
    @just _fix-go

# Fix targets. Valid: "backend", "cli", "types", "go", "all".
[group('quality')]
fix target="all":
    @just "_fix-{{ target }}"

# -----------------------------------------------------------------------------
# Security
# -----------------------------------------------------------------------------

# Run Snyk against all projects including dev dependencies
[group('security')]
_snyk-scan:
    snyk test --all-projects --dev --policy-path=.snyk

# Snyk targets. Valid: "scan".
[group('security')]
snyk target="scan":
    @just "_snyk-{{ target }}"

# -----------------------------------------------------------------------------
# Dependencies
# -----------------------------------------------------------------------------

# Install frontend dependencies
[group('deps')]
_deps-install-viteplus:
    vp migrate

_deps-install-frontend:
    vp install

# Install tests dependencies
[group('deps')]
_deps-install-tests:
    vp -C tests install
    vp -C tests exec playwright install --with-deps chromium firefox

# Install backend Go dependencies
[group('deps')]
_deps-install-backend:
    cd backend && go mod download && go mod tidy && go mod verify
    go work sync

# Install CLI Go dependencies
[group('deps')]
_deps-install-cli:
    cd cli && go mod download && go mod tidy && go mod verify
    go work sync

# Install types Go dependencies
[group('deps')]
_deps-install-types:
    cd types && go mod download && go mod tidy && go mod verify
    go work sync

# Install all Go dependencies
[group('deps')]
_deps-install-go: _deps-install-backend _deps-install-cli _deps-install-types

# Install all Node.js dependencies
[group('deps')]
_deps-install-node: _deps-install-frontend _deps-install-tests _deps-install-viteplus

# Install all dependencies
[group('deps')]
_deps-install-all: _deps-install-node _deps-install-go

# Update frontend dependencies
[group('deps')]
_deps-update-frontend:
    vp update

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

# Dedupe all pnpm workspace dependencies
[group('deps')]
_deps-dedupe-node:
    vp dedupe

[group('deps')]
_deps-dedupe-all: _deps-dedupe-node

# Deps targets. Valid: "install [frontend|tests|backend|cli|types|go|node|all]", "update [frontend|backend|pnpm|all]", "dedupe [node|go|all]"
[group('deps')]
deps action="update" target="all":
    @just "_deps-{{ action }}-{{ target }}"

# -----------------------------------------------------------------------------
# Code generation and docs
# -----------------------------------------------------------------------------

# Generate edge tunnel protobuf/gRPC code.
[group('codegen')]
_generate-proto:
    cd {{ edge_proto_dir }} && go run github.com/bufbuild/buf/cmd/buf@latest lint
    cd {{ edge_proto_dir }} && go run github.com/bufbuild/buf/cmd/buf@latest generate

# Generate targets. Valid: "proto".
[group('codegen')]
generate target:
    @just "_generate-{{ target }}"

# Generate the docs config schema JSON.
[group('docs')]
_docs-config output="" source_root=".":
    #!/usr/bin/env bash
    set -euo pipefail

    cmd=(go run -tags exclude_frontend ./backend/cmd config-schema --source-root "{{ source_root }}")
    if [ -n "{{ output }}" ]; then
        cmd+=(--output "{{ output }}")
    fi

    "${cmd[@]}"

# Docs targets. Example: just docs config
[group('docs')]
docs target *args:
    @just "_docs-{{ target }}" {{ args }}

# -----------------------------------------------------------------------------
# Localization
# -----------------------------------------------------------------------------

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

    formatting_path="frontend/src/lib/utils/formatting.ts"
    if [ ! -f "$formatting_path" ]; then
        echo "Warning: $formatting_path not found; add the date-fns loader for '{{ locale }}' manually."
    elif rg -q "date-fns/locale/{{ locale }}'" "$formatting_path"; then
        echo "date-fns loader for '{{ locale }}' already present in $formatting_path"
    else
        f_start="$(rg -n -F "const dateFnsLocaleLoaders" "$formatting_path" | head -n1 | cut -d: -f1)"
        if [ -z "$f_start" ]; then
            echo "Warning: unable to find dateFnsLocaleLoaders in $formatting_path; add '{{ locale }}' manually."
        else
            f_end="$(awk -v s="$f_start" 'NR>s && $0 ~ /^[[:space:]]*};/ { print NR; exit }' "$formatting_path")"
            if [ -z "$f_end" ]; then
                echo "Warning: unable to find end of dateFnsLocaleLoaders in $formatting_path; add '{{ locale }}' manually."
            else
                f_const_indent="$(sed -n "${f_start}p" "$formatting_path" | sed -E 's/^([[:space:]]*).*/\1/')"
                f_entry_indent="$(sed -n "$((f_start+1)),$((f_end-1))p" "$formatting_path" | awk 'NF { match($0, /^[[:space:]]*/); print substr($0, RSTART, RLENGTH); exit }')"
                if [ -z "$f_entry_indent" ]; then
                    f_entry_indent="${f_const_indent}	"
                fi

                f_entries="$(mktemp)"
                while IFS= read -r line; do
                    if [[ $line =~ ^[[:space:]]*\'?([^\'\":]+)\'?:.*date-fns/locale/([A-Za-z-]+) ]]; then
                        key="${BASH_REMATCH[1]}"
                        mod="${BASH_REMATCH[2]}"
                        if [ "$key" != "{{ locale }}" ]; then
                            printf '%s\t%s\n' "$key" "$mod" >> "$f_entries"
                        fi
                    fi
                done < <(sed -n "$((f_start+1)),$((f_end-1))p" "$formatting_path")
                printf '%s\t%s\n' "{{ locale }}" "{{ locale }}" >> "$f_entries"

                f_block="$(sed -n "${f_start}p" "$formatting_path")"
                f_block+=$'\n'
                while IFS=$'\t' read -r key mod; do
                    [ -z "$key" ] && continue
                    if [[ $key =~ ^[A-Za-z_$][A-Za-z0-9_$]*$ ]]; then
                        out_key="$key"
                    else
                        out_key="'${key}'"
                    fi
                    f_block+="${f_entry_indent}${out_key}: () => resolveDateFnsLocale(() => import('date-fns/locale/${mod}')),"
                    f_block+=$'\n'
                done < <(LC_ALL=C sort -f -t $'\t' -k1,1 "$f_entries")
                f_block+="${f_const_indent}};"
                rm -f "$f_entries"

                f_tmp="$(mktemp)"
                sed -n "1,$((f_start-1))p" "$formatting_path" > "$f_tmp"
                printf '%s\n' "$f_block" >> "$f_tmp"
                tail -n "+$((f_end+1))" "$formatting_path" >> "$f_tmp"
                mv "$f_tmp" "$formatting_path"
                echo "Added date-fns loader for '{{ locale }}' to $formatting_path"

                if [ ! -e "frontend/node_modules/date-fns/locale/{{ locale }}.js" ] && [ ! -d "frontend/node_modules/date-fns/locale/{{ locale }}" ]; then
                    echo "Warning: date-fns may not ship a '{{ locale }}' locale (check the module name, e.g. en uses en-US)."
                fi
            fi
        fi
    fi

    if [ -f "$target_file" ]; then
        echo "Messages file already exists, not overwriting: $target_file"
    else
        cp "$base_file" "$target_file"
        echo "Created messages file: $target_file"
    fi

# -----------------------------------------------------------------------------
# Benchmarks
# -----------------------------------------------------------------------------

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

# -----------------------------------------------------------------------------
# Deploy
# -----------------------------------------------------------------------------

# Deploy a local DinD engine plus a locally built Arcane edge agent.
# The swarm form joins the engine to the host swarm; the normal agent form leaves
# it outside the swarm so Arcane Easy Join can perform the join later.
#
# Usage:

# just deploy swarm agent [agent_token] [manager_url] [node_name] [agent_name] [dind_image] [local_image]
# just deploy agent [agent_token] [manager_url] [node_name] [agent_name] [dind_image] [local_image]
[group('deploy')]
_deploy-agent join_swarm agent_token="" manager_url="http://host.docker.internal:3552" node_name="arcane-agent-1" agent_name="arcane-agent" dind_image="docker:29-dind" local_image="ghcr.io/getarcaneapp/agent:local":
    #!/usr/bin/env bash
    set -euo pipefail

    if ! command -v docker >/dev/null 2>&1; then
        echo "docker is required"
        exit 1
    fi

    if ! docker info >/dev/null 2>&1; then
        echo "docker daemon is not running"
        exit 1
    fi

    if [ "{{ join_swarm }}" = "true" ]; then
        swarm_state="$(docker info 2>/dev/null | awk -F': ' '/Swarm:/{print tolower($2); exit}')"
        swarm_control="$(docker info 2>/dev/null | awk -F': ' '/Is Manager:/{print tolower($2); exit}')"
        if [ "$swarm_state" != "active" ] || [ "$swarm_control" != "true" ]; then
            echo "host docker must already be an active swarm manager"
            exit 1
        fi
    fi

    echo "Building local agent image {{ local_image }}..."
    docker buildx build --load -f docker/Dockerfile-agent -t "{{ local_image }}" .

    if docker inspect "{{ node_name }}" >/dev/null 2>&1; then
        echo "Reusing existing DinD engine {{ node_name }}..."
        docker start "{{ node_name }}" >/dev/null 2>&1 || true
    else
        echo "Starting DinD engine {{ node_name }} from {{ dind_image }}..."
        docker run -d \
            --privileged \
            --name "{{ node_name }}" \
            --hostname "{{ node_name }}" \
            --restart unless-stopped \
            --add-host=host.docker.internal:host-gateway \
            -e DOCKER_TLS_CERTDIR= \
            "{{ dind_image }}"
    fi

    echo "Waiting for inner Docker daemon..."
    for _ in $(seq 1 30); do
        if docker exec "{{ node_name }}" docker info >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done

    if ! docker exec "{{ node_name }}" docker info >/dev/null 2>&1; then
        echo "inner docker daemon did not become ready"
        exit 1
    fi

    local_node_state="$(docker exec "{{ node_name }}" docker info --format '{{ "{{.Swarm.LocalNodeState}}" }}')"
    if [ "{{ join_swarm }}" = "true" ]; then
        if [ "$local_node_state" != "active" ]; then
            join_token="$(docker swarm join-token -q worker)"
            echo "Joining {{ node_name }} to host swarm..."
            docker exec "{{ node_name }}" docker swarm join --token "$join_token" host.docker.internal:2377
        else
            echo "{{ node_name }} is already part of the swarm."
        fi
    else
        if [ "$local_node_state" = "active" ]; then
            echo "{{ node_name }} is already part of a swarm. Remove it first with: just remove agent {{ node_name }} {{ agent_name }}"
            exit 1
        fi
        echo "Leaving {{ node_name }} outside the swarm for Arcane Easy Join."
    fi

    current_node_id="$(docker exec "{{ node_name }}" docker info --format '{{ "{{.Swarm.NodeID}}" }}')"
    if [ -n "$current_node_id" ]; then
        echo "Current swarm node ID: $current_node_id"
    fi

    if [ -n "{{ agent_token }}" ] && command -v sqlite3 >/dev/null 2>&1 && [ -f backend/data/arcane.db ]; then
        expected_node_id="$(sqlite3 backend/data/arcane.db "SELECT COALESCE(swarm_node_id, '') FROM environments WHERE access_token = '{{ agent_token }}' LIMIT 1;")"
        if [ -n "$expected_node_id" ] && [ "$expected_node_id" != "$current_node_id" ]; then
            echo "agent token belongs to swarm node $expected_node_id, but {{ node_name }} is $current_node_id"
            if [ "{{ join_swarm }}" = "true" ]; then
                echo "Create a fresh Remote Environment from the Connect Agent dialog for node {{ node_name }}, then rerun this command with its token."
            else
                echo "Create a fresh visible Edge Agent under Environments, then rerun this command with its token."
            fi
            exit 1
        fi
    fi

    if [ -z "{{ agent_token }}" ]; then
        echo ""
        if [ "{{ join_swarm }}" = "true" ]; then
            echo "Swarm worker {{ node_name }} is ready, but no Arcane agent token was provided."
            echo "Open Arcane, click Connect Agent for node {{ node_name }} (node ID: $current_node_id), create its Remote Environment, copy the generated token, and rerun:"
            echo "  just deploy swarm agent <arcane_agent_token> {{ manager_url }} {{ node_name }} {{ agent_name }} {{ dind_image }} {{ local_image }}"
        else
            echo "Docker engine {{ node_name }} is ready, but no Arcane agent token was provided."
            echo "Create a visible Edge Agent under Environments, copy its token, and rerun:"
            echo "  just deploy agent <arcane_agent_token> {{ manager_url }} {{ node_name }} {{ agent_name }} {{ dind_image }} {{ local_image }}"
        fi
        exit 0
    fi

    echo "Loading local agent image into {{ node_name }}..."
    docker save "{{ local_image }}" | docker exec -i "{{ node_name }}" docker load >/dev/null

    echo "Starting agent container {{ agent_name }} inside {{ node_name }}..."
    docker exec "{{ node_name }}" sh -lc '
        docker rm -f "{{ agent_name }}" >/dev/null 2>&1 || true
        docker run -d \
          --name "{{ agent_name }}" \
          --restart unless-stopped \
          -e EDGE_AGENT=true \
          -e EDGE_TRANSPORT=poll \
          -e AGENT_TOKEN="{{ agent_token }}" \
          -e MANAGER_API_URL="{{ manager_url }}" \
          -v /var/run/docker.sock:/var/run/docker.sock \
          -v arcane-data:/app/data \
          "{{ local_image }}"
    '

    echo ""
    if [ "{{ join_swarm }}" = "true" ]; then
        echo "Swarm worker {{ node_name }} and local agent {{ agent_name }} are up."
    else
        echo "Docker engine {{ node_name }} and local agent {{ agent_name }} are up."
        echo "The engine is connected to Arcane and remains outside the swarm."
    fi
    echo "Verify:"
    if [ "{{ join_swarm }}" = "true" ]; then
        echo "  docker node ls"
    else
        echo "  docker exec {{ node_name }} docker info --format '{{ "{{.Swarm.LocalNodeState}}" }}'"
    fi
    echo "  docker exec {{ node_name }} docker ps"
    echo "  docker exec {{ node_name }} docker logs {{ agent_name }}"

# Remove a local DinD engine plus its Arcane edge agent.
#
# Usage:
#

# just remove swarm agent [node_name] [agent_name]
# just remove agent [node_name] [agent_name]
[group('deploy')]
_remove-agent node_name="arcane-agent-1" agent_name="arcane-agent":
    #!/usr/bin/env bash
    set -euo pipefail

    if ! command -v docker >/dev/null 2>&1; then
        echo "docker is required"
        exit 1
    fi

    if ! docker info >/dev/null 2>&1; then
        echo "docker daemon is not running"
        exit 1
    fi

    if ! docker inspect "{{ node_name }}" >/dev/null 2>&1; then
        echo "container {{ node_name }} does not exist"
        exit 1
    fi

    echo "Stopping inner agent container {{ agent_name }} inside {{ node_name }}..."
    docker exec "{{ node_name }}" sh -lc 'docker rm -f "{{ agent_name }}" >/dev/null 2>&1 || true'

    node_id=""
    if docker exec "{{ node_name }}" docker info >/dev/null 2>&1; then
        local_node_state="$(docker exec "{{ node_name }}" docker info --format '{{ "{{.Swarm.LocalNodeState}}" }}' 2>/dev/null || true)"
        if [ "$local_node_state" = "active" ]; then
            node_id="$(docker exec "{{ node_name }}" docker info --format '{{ "{{.Swarm.NodeID}}" }}' 2>/dev/null || true)"
            echo "Leaving swarm from {{ node_name }}..."
            docker exec "{{ node_name }}" docker swarm leave -f >/dev/null 2>&1 || true
        fi
    fi

    if [ -n "$node_id" ]; then
        echo "Removing swarm node $node_id from host manager..."
        docker node rm -f "$node_id" >/dev/null 2>&1 || true
    fi

    echo "Removing DinD engine container {{ node_name }} from host..."
    docker rm -f "{{ node_name }}" >/dev/null

    echo ""
    echo "Removed Docker engine {{ node_name }} and inner agent {{ agent_name }}."

# Deploy targets. Examples: just deploy agent [agent_token], just deploy swarm agent [agent_token]
[group('deploy')]
deploy target *args:
    #!/usr/bin/env bash
    set -euo pipefail
    set -- {{ args }}
    case "{{ target }}" in
        agent)
            just _deploy-agent false "$@"
            ;;
        swarm)
            if [ "${1:-}" != "agent" ]; then
                echo "usage: just deploy swarm agent [agent_token] [manager_url] [node_name] [agent_name] [dind_image] [local_image]"
                exit 1
            fi
            shift
            just _deploy-agent true "$@"
            ;;
        *)
            echo "unknown deploy target: {{ target }}"
            exit 1
            ;;
    esac

# Remove deployed targets. Examples: just remove agent, just remove swarm agent
[group('deploy')]
remove target *args:
    #!/usr/bin/env bash
    set -euo pipefail
    set -- {{ args }}
    case "{{ target }}" in
        agent)
            just _remove-agent "$@"
            ;;
        swarm)
            if [ "${1:-}" != "agent" ]; then
                echo "usage: just remove swarm agent [node_name] [agent_name]"
                exit 1
            fi
            shift
            just _remove-agent "$@"
            ;;
        *)
            echo "unknown remove target: {{ target }}"
            exit 1
            ;;
    esac

# -----------------------------------------------------------------------------
# Release
# -----------------------------------------------------------------------------

# Compute the next semver next-image version (e.g. 2.4.0-next.1) from unreleased commits.
# Bump rules: breaking -> major, feat -> minor, other included commits -> patch.
# The -next.N counter continues from tags already published to GHCR; set
# GHCR_TAGS (newline separated) to bypass the registry query for testing.
#
# Usage: just next-image-version [github-output]
[group('release')]
next-image-version mode="":
    #!/usr/bin/env bash
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

    GHCR_IMAGE="${GHCR_IMAGE:-getarcaneapp/arcane}"
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
    MAX_N=$(grep -E "^v${ESCAPED_BASE}-next\.[0-9]+$" <<<"$TAGS" | sed -E 's/.*-next\.//' | sort -n | tail -n1 || true)
    COUNTER=$(( ${MAX_N:-0} + 1 ))

    VERSION="${NEXT_BASE}-next.${COUNTER}"
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

[group('release')]
_utils-list-feats:
    #!/usr/bin/env bash
    set -euo pipefail

    if ! command -v gh >/dev/null 2>&1; then
        echo "GitHub CLI is required. Install it from https://cli.github.com/." >&2
        exit 1
    fi

    if ! gh auth status --hostname github.com >/dev/null 2>&1; then
        echo "GitHub CLI is not authenticated. Run: gh auth login" >&2
        exit 1
    fi

    discussions=$(
        gh api graphql \
            --paginate \
            --slurp \
            -f owner=getarcaneapp \
            -f name=arcane \
            -F endCursor=null \
            -f query='query($owner: String!, $name: String!, $endCursor: String) { repository(owner: $owner, name: $name) { discussions(first: 100, after: $endCursor) { nodes { number title url category { slug } isAnswered upvoteCount reactionGroups { content users { totalCount } } } pageInfo { hasNextPage endCursor } } } }' \
        | jq -r 'map(.data.repository.discussions.nodes[] | select(.category.slug == "feature-requests") | . + {upvotes: (.upvoteCount + ([.reactionGroups[]? | select(.content == "THUMBS_UP") | .users.totalCount] | add // 0))}) | sort_by(.upvotes, .number) | reverse | .[] | [.upvotes, .number, (if .isAnswered then "answered" else "open" end), .title, .url] | @tsv'
    )

    if [ -z "$discussions" ]; then
        echo "No Feature discussions found."
        exit 0
    fi

    echo "Feature discussions by votes (upvotes + 👍):"
    echo ""

    while IFS=$'\t' read -r votes number status title url; do
        printf "%3d votes - #%-4s [%s] %s\n" "$votes" "$number" "$status" "$title"
        printf "         %s\n\n" "$url"
    done <<< "$discussions"

[group('release')]
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

    if [ "$TEST" == true ]; then
        echo "Test mode: no changes were made."
    fi

# Utils targets. Valid: "list-feats", "list-fixes".
[group('release')]
utils target *args:
    @just "_utils-{{ target }}" {{ args }}

# -----------------------------------------------------------------------------
# Repository maintenance
# -----------------------------------------------------------------------------

# Clean build artifacts
[group('maintenance')]
_repo-clean:
    rm -rf frontend/.svelte-kit frontend/build backend/.bin
    find . -type d -name node_modules -prune -exec rm -rf {} \;

# Repo targets. Valid: "clean".
[group('maintenance')]
repo target="clean":
    @just "_repo-{{ target }}"
