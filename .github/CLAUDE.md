# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Arcane is a modern Docker management UI with a Go backend and SvelteKit frontend. It manages containers, images, volumes, networks, stacks, and tracks image updates.

## Commands

### Development

```bash
./scripts/development/dev.sh start    # Start Docker-based dev environment (recommended)
./scripts/development/dev.sh stop|restart|rebuild|clean
```

### Backend (Go)

```bash
cd backend
go build ./...                        # Build all modules
go test ./...                         # Run tests
golangci-lint run                     # Lint
```

### Frontend (SvelteKit)

```bash
pnpm -w -C frontend dev               # Dev server (port 3000)
pnpm -w -C frontend build             # Production build
pnpm -w -C frontend check             # Type check
pnpm -w format                        # Format all code
```

### E2E Tests (Playwright)

```bash
pnpm test                             # Run tests
```

## Architecture

**Three Go modules** unified via `go.work`:

- `backend/` - API server, services, models
- `cli/` - Command-line tool (uses Cobra)
- `types/` - Shared type definitions

**Frontend**: SvelteKit in `frontend/` with pnpm

### Backend Structure

```
backend/internal/
├── bootstrap/        # App initialization & DI wiring (start here)
├── api/              # HTTP handlers (*_handler.go)
├── services/         # Business logic (*_service.go)
├── models/           # GORM database models
├── dto/              # Request/response DTOs
├── huma/             # Huma v2 API handlers (newer pattern)
└── job/              # Background job scheduling (gocron v2)
```

### Frontend Structure

```
frontend/src/
├── routes/(app)/     # Main app pages (dashboard, containers, images, etc.)
├── routes/(auth)/    # Auth pages
├── lib/components/   # Reusable Svelte components (shadcn-svelte)
├── lib/services/api/ # API service classes (Axios)
├── lib/stores/       # Svelte stores
└── lib/types/        # TypeScript types
```

## Key Patterns

### Backend

- **Thin handlers**: HTTP handlers call services; business logic lives in `*_service.go`
- **Bootstrap DI**: All wiring happens in `internal/bootstrap/` - check here for how services are instantiated
- **DTOs for boundaries**: Use `internal/dto/` for API request/response types
- **Structured logging**: Use `slog` with context
- **Error wrapping**: `fmt.Errorf("context: %w", err)`

### Frontend (Svelte 5 Only)

```svelte
<!-- Props -->
let { prop1, prop2 } = $props();

<!-- State -->
let count = $state(0);

<!-- Derived -->
let doubled = $derived(count * 2);

<!-- Effects -->
$effect(() => { /* side effects */ });
```

**Do NOT update state values in an effect**

**Do NOT use Svelte 4 syntax** (`export let`, `on:click`, `$:`, etc.)

### Database

- GORM with SQLite or PostgreSQL
- Models include `BaseModel` (UUID, timestamps)
- Use proper preloading for relationships

## Anti-Patterns

- Don't put business logic in handlers
- Don't use Svelte 4 syntax anywhere
- Don't hardcode registry-specific logic (support generic OCI patterns)
- Don't bypass TypeScript with `any`
- Don't add unnecessary comments

## Container Registry Integration

When working with registries:

- Use generic authentication (bearer tokens, basic auth)
- Support multiple providers (Docker Hub, GHCR, custom OCI)
- Use case-insensitive header checking
