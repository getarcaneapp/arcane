set working-directory := './'

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
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64 -f 'docker/Dockerfile' -t "{{ tag }}" .

# Build agent container image
[group('build')]
_build-image-agent tag="arcane-agent:latest" flag='':
    docker buildx build {{ if flag == "--push" { "--push" } else { "" } }} --platform linux/arm64,linux/amd64 -f 'docker/Dockerfile-agent' -t "{{ tag }}" .

# Build both frontend and backend
[group('build')]
_build-all:
    @just _build-frontend
    @just _build-backend

# Build targets. Valid: "single frontend", "single backend", "single all", "image manager [tag] [--push]", "image agent [tag] [--push]"
[group('build')]
build buildtype type tag="" flag="":
    @if [ "{{ buildtype }}" = "single" ]; then just _build-{{ type }}; elif [ "{{ buildtype }}" = "image" ]; then just _build-image-{{ type }} "{{ if tag != "" { tag } else if type == "manager" { "arcane:latest" } else { "arcane-agent:latest" } }}" "{{ flag }}"; fi

# --- Testing ---

# Run Playwright E2E tests
[group('tests')]
_test-e2e:
    pnpm -C tests test

# Run backend Go tests
[group('tests')]
_test-backend:
    cd backend && go test -tags=exclude_frontend ./... -race -coverprofile=coverage.txt -covermode=atomic -v

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
    pnpm exec playwright install --with-deps chromium

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
    cd backend && golangci-lint run ./...

# Lint Go CLI
[group('lint')]
_lint-cli:
    cd cli && golangci-lint run ./...

# Lint Types
[group('lint')]
_lint-types:
    cd types && golangci-lint run ./...

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
_format-all:
    @just _format-frontend
    @just _format-go
    @just _format-just

# Format targets. Valid: "frontend", "go", "just", "all".
[group('format')]
format target="all":
    @just "_format-{{ target }}"

# Clean build artifacts
[group('repo')]
_repo-clean:
    rm -rf frontend/.svelte-kit frontend/build backend/.bin
    find . -type d -name node_modules -prune -exec rm -rf {} \;

# Repo targets. Valid: "clean".
[group('repo')]
repo target="clean":
    @just "_repo-{{ target }}"
