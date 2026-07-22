# Arcane AI Agent Instructions

> **All AI agents must conform to [AI_POLICY.md](./AI_POLICY.md)**

Arcane is a Docker management UI: **Go backend** (Echo + Huma v2), **SvelteKit v3 frontend** (Svelte 5), optional headless agent, Cobra CLI. Three Go modules via `go.work`: `backend/`, `cli/`, `types/`. Domain docs: `CONTEXT.md`.

## Development Environment

```bash
./scripts/development/dev.sh start|stop|restart|rebuild|clean|logs
# Frontend: http://localhost:3000 (Vite HMR) | Backend: http://localhost:3552 (Air)
```

## ⚠️ Golden Rules — READ FIRST

1. **ALWAYS update existing code in place.** Never create a new function, service, helper, or API wrapper when one already exists that does the same thing. Find the existing code and modify it.
2. **NEVER create stub, shim, or pass-through helper functions.** If a `pkg/` helper or service method exists, call it directly. Do not write a thin wrapper that just forwards to it.
3. **NEVER duplicate functionality.** Before writing anything, search `pkg/`, `internal/services/`, and `frontend/src/lib/services/` for existing implementations.
4. **NEVER create a new API standard.** If the codebase uses Huma v2 typed handlers on Echo, do not introduce Gin, raw `http.HandlerFunc`, or untyped patterns. Extend the existing standard.
5. If you need a paragraph comment in order to justify a function, it is wrong. Fix the code.

## Architecture Overview

### Backend (`backend/`)

```
cmd/                  # entrypoint
api/                  # HTTP API surface
├── api.go            # Huma v2 on Echo via humaecho — register handlers here
├── handlers/         # Thin Huma handlers → call services
├── middleware/       # Huma auth bridge
└── ws/               # Echo WebSocket handlers
frontend/             # embedded SvelteKit build
internal/
├── bootstrap/        # DI wiring, router setup — START HERE
├── config/           # env config
├── database/         # GORM + migrations
├── middleware/       # Echo middleware (auth, CORS, rate limit)
├── models/           # GORM models (embed BaseModel)
└── services/         # Business logic — *_service.go
pkg/
├── authz/            # Permissions + access policy
├── dockerutil/       # Docker helpers (names, labels, logs, mounts)
├── libarcane/        # Core libraries (compose, edge, image update, build, swarm)
├── pagination/       # Search/filter/sort/pagination
├── projects/         # Compose parsing, image refs, discovery
├── scheduler/        # Cron jobs
└── utils/            # Shared helpers (strings, cache, ptr, httpx)
resources/            # migrations, images, email templates
```

**Key patterns:**

- Echo is the HTTP router. Huma v2 is the typed REST/OpenAPI layer mounted via `humaecho.NewWithGroup`.
- Handlers are thin: extract typed input → call service → return typed response.
- Services use constructor injection (see `bootstrap.go`).
- Direct Echo routes only for WebSockets, streaming, diagnostics, webhooks, Playwright routes, and embedded frontend.
- Use `slog` for logging, `fmt.Errorf("context: %w", err)` for errors.

**Reuse `pkg/` helpers — call directly, never wrap:**

- Docker → `pkg/dockerutil`: `ContainerNameFromNames`, `ComposeProjectLabel`/`ComposeServiceLabel`, `StreamContainerLogs`/`ReadAllLogs`
- Compose → `pkg/projects`: `ImageRefsFromComposeServices`/`ImageRefsFromRuntimeServices`
- Pagination → `pkg/pagination`: `SearchOrderAndPaginate`, `PaginateAndSortDB`
- Shared → `pkg/utils`

### Frontend (`frontend/src/`)

SvelteKit v3 — config lives in `vite.config.ts` (no `svelte.config.js`). Routes use `+page.svelte`/`+page.ts`/`+layout.svelte`/`+layout.ts` naming.

```
routes/(app)/         # App pages (dashboard, containers, images, etc.)
routes/(auth)/        # Auth pages
lib/components/       # Reusable components (shadcn-svelte)
lib/services/         # API services extending BaseAPIService
lib/stores/           # Svelte stores (*.store.svelte using runes)
lib/types/            # TypeScript types
../messages/en.json   # i18n source strings (Paraglide)
```

### Shared Types (`types/`)

Domain types shared between backend and CLI. Each domain has its own package.

### CLI (`cli/`)

Cobra app. Commands in `cli/pkg/<domain>/`, helpers in `cli/internal/`, types from `types/`.

## Critical Patterns

### Svelte 5 Runes ONLY

```svelte
<script lang="ts">
  let { name }: { name: string } = $props();
  let count = $state(0);
  let doubled = $derived(count * 2);
  $effect(() => { /* ... */ });
</script>
<button onclick={handleClick}>Click</button>
```

**NEVER:** `export let`, `on:click`, `$:`, `$$props`, `$$restProps`, old slot syntax.

### API Service Pattern

```typescript
export class ContainerService extends BaseAPIService {
  async getContainers(options?: SearchPaginationSortRequest) {
    const envId = await environmentStore.getCurrentEnvironmentId();
    const params = transformPaginationParams(options);
    return this.api.get(`/environments/${envId}/containers`, { params });
  }
}
export const containerService = new ContainerService();
```

### Huma Handler Pattern

```go
// 1. Define typed input/output structs
type ListContainersInput struct {
    EnvironmentID string `path:"id" doc:"Environment ID"`
    Search        string `query:"search" doc:"Search query"`
    Limit         int    `query:"limit" default:"20"`
}

// 2. Register in handlers via huma.Register (in registerHandlers in api.go)
// 3. Handler body: call service, return response
```

### GORM Model Pattern

```go
type Stack struct {
    models.BaseModel
    Name string `json:"name" gorm:"column:name" sortable:"true"`
}
func (Stack) TableName() string { return "stacks" }
```

Use `Preload` for relationships. Use `models.JSON` for JSON fields, `models.StringSlice` for string arrays.

### i18n — No Raw Strings

All user-facing text goes in `frontend/messages/en.json`, accessed via Paraglide messages:

```svelte
<Button>{m.common_save_changes()}</Button>
```

**AI Agent Rule for i18n**:

- Never use hardcoded English strings in frontend UI components (`.svelte` files). This includes text inside HTML tags and text assigned to variables, props, or state that is rendered in the UI.
- Before adding a page-specific message key, search `frontend/messages/en.json` for the same user-facing text and reuse an existing shared key when the grammatical purpose matches.
- Always define new keys ONLY in `frontend/messages/en.json` and use `m.*()`. Do NOT edit other language files (e.g., `fr.json`, `es.json`) because translations are handled automatically by Crowdin via pull requests.

## RBAC Architecture

Three layers — keep them separate:

- **Permission catalog** (`backend/pkg/authz/catalog.go`, `permissions.go`): raw capabilities, scope taxonomy, constants.
- **Access-surface registry** (`backend/pkg/authz/access_policy.go`): page/route/landing reachability metadata.
- **Frontend UX gates**: navigation config with `accessSurfaceId` references. Backend enforcement via `RequirePermission`/`RequireGlobalAdmin` remains authoritative.

Admin semantics from `PermissionSet.IsGlobalAdmin()` (backend) / `isGlobalAdmin` in user DTOs (frontend). Do not infer admin from role IDs in frontend auth checks.

## Multi-Environment Support

- Environment ID `"0"` = local Docker socket
- All API calls include `/environments/{id}/` prefix
- `environmentStore.ready` is a Promise — await before use
- On environment change, detail pages redirect to list pages

## Background Jobs

1. Implement `Job` interface in `backend/pkg/scheduler/` (`Name()`, `Schedule()` → 6-field cron, `Run()`)
2. Register in `jobs_bootstrap.go`

## Image & Container Updates

- `backend/pkg/libarcane/imageupdate/` — digest checks, registry queries, label checks, sorter
- `backend/internal/services/image_update_service.go` — check + persist update records
- `backend/internal/services/updater_service.go` — apply updates
- `backend/api/handlers/image_updates.go` + `updater.go` — API endpoints
- `backend/pkg/scheduler/` — `image_polling_job.go`, `auto_update_job.go`, `auto_heal_job.go`

## Agent Modes

- **Edge**: Agent dials out to manager via WebSocket/gRPC tunnel. Config: `EDGE_AGENT=true`, `MANAGER_API_URL`, `AGENT_TOKEN`.
- **Direct**: Agent runs HTTP server on TCP 3553, manager dials in. Config: `AGENT_MODE=true`, `AGENT_TOKEN`. No `MANAGER_API_URL`.

## Testing

```bash
cd backend && go test ./...        # unit tests (in-memory SQLite, testify)
just test e2e                      # Playwright E2E
just lint frontend                 # Svelte type checking
```

## Anti-Patterns

| ❌ Don't                                         | ✅ Do                                              |
| ------------------------------------------------ | -------------------------------------------------- |
| Business logic in handlers                       | Thin handlers → services                           |
| Gin router/middleware                            | Huma on Echo                                       |
| Svelte 4 syntax (`export let`, `on:click`, `$:`) | Svelte 5 runes (`$props()`, `onclick`, `$derived`) |
| Hardcoded API paths without env ID               | Include `/environments/{id}/`                      |
| Models without `BaseModel`                       | Embed `models.BaseModel`                           |
| TypeScript `any`                                 | Proper types from `$lib/types`                     |
| Hardcoded UI strings                             | Paraglide messages via `m.*()`                     |
| Wrapping `pkg/` helpers                          | Call existing helpers directly                     |
| New API standard (raw HTTP, untyped)             | Extend existing Huma typed handlers                |
