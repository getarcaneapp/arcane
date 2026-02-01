# Arcane AI Agent Instructions

Arcane is a modern Docker management UI with a **Go backend** (Huma v2 API), **SvelteKit frontend** (Svelte 5), and an optional headless agent. Three Go modules unified via `go.work`: `backend/`, `cli/`, `types/`.

## Development Environment

```bash
./scripts/development/dev.sh start    # Start Docker-based dev environment (hot reload)
./scripts/development/dev.sh stop|restart|rebuild|clean|logs
```

- Frontend: http://localhost:3000 (Vite HMR)
- Backend: http://localhost:3552 (Air hot reload)

## Architecture Overview

### Backend (`backend/`)

```
internal/
├── bootstrap/        # App initialization & DI wiring — START HERE for understanding how services connect
├── huma/handlers/    # HTTP handlers (Huma v2) — thin wrappers that call services
├── services/         # Business logic — *_service.go files contain all domain logic
├── models/           # GORM database models (include BaseModel for UUID, timestamps)
├── config/           # Environment configuration
└── middleware/       # Auth, logging, rate limiting
```

**Key patterns:**
- Handlers are thin: extract user from context, call service, return response
- Services receive dependencies via constructor injection (see [bootstrap.go](backend/internal/bootstrap/bootstrap.go))
- Use `slog` for structured logging with context
- Error wrapping: `fmt.Errorf("context: %w", err)`

### Frontend (`frontend/src/`)

```
routes/(app)/         # Main app pages (dashboard, containers, images, etc.)
routes/(auth)/        # Auth pages  
lib/components/       # Reusable Svelte components (shadcn-svelte based)
lib/services/         # API service classes extending BaseAPIService
lib/stores/           # Svelte stores (*.store.svelte files use runes)
lib/types/            # TypeScript types
```

### Shared Types (`types/`)

Domain types shared between backend and CLI. Each domain has its own package (e.g., `types/container/`, `types/image/`).

## Critical Patterns

### Svelte 5 ONLY — No Svelte 4 Syntax

```svelte
<!-- Props: use $props() -->
let { prop1, prop2 }: { prop1: string; prop2?: number } = $props();

<!-- State: use $state() -->
let count = $state(0);

<!-- Derived values: use $derived() or $derived.by() -->
let doubled = $derived(count * 2);
let computed = $derived.by(() => complexCalculation());

<!-- Side effects: use $effect() -->
$effect(() => { /* runs when dependencies change */ });
```

**NEVER use:** `export let`, `on:click` (use `onclick`), `$:`, `$$props`, `$$restProps`, slot syntax

Example component: [job-card.svelte](frontend/src/lib/components/job-card/job-card.svelte)

### API Service Pattern

Frontend services extend `BaseAPIService` and use `environmentStore` for multi-environment support:

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

Handlers use typed input/output structs with struct tags for validation:

```go
type ListContainersInput struct {
    EnvironmentID string `path:"id" doc:"Environment ID"`
    Search        string `query:"search" doc:"Search query"`
    Limit         int    `query:"limit" default:"20" doc:"Limit"`
}
```

Register handlers in [backend/internal/huma/handlers/](backend/internal/huma/handlers/).

## Testing

```bash
# Backend unit tests
cd backend && go test ./...

# E2E tests (Playwright)
pnpm test                    # from project root

# Frontend type checking
pnpm -C frontend check:ci
```

Backend tests use in-memory SQLite and testify. See [auth_service_test.go](backend/internal/services/auth_service_test.go) for patterns.

## Anti-Patterns to Avoid

- Business logic in handlers — move to services
- Svelte 4 syntax anywhere
- Hardcoded registry logic — use generic OCI patterns
- TypeScript `any` without justification
- Unnecessary comments explaining obvious code

## Container Registry Integration

- Use generic authentication (bearer tokens, basic auth)
- Support multiple providers (Docker Hub, GHCR, custom OCI)
- Use case-insensitive header checking

## Multi-Environment Support

Arcane supports managing multiple Docker environments (local + remote agents). The frontend uses `environmentStore` to track the active environment:

```typescript
// LOCAL_DOCKER_ENVIRONMENT_ID = '0' is the local Docker socket
const envId = await environmentStore.getCurrentEnvironmentId();

// All API calls include environment ID in the path
this.api.get(`/environments/${envId}/containers`);
```

**Key patterns:**
- Environment ID `"0"` = local Docker connection  
- Remote environments connect via agents (standard or edge)
- `environmentStore.ready` is a Promise — await before accessing environment-specific data
- When environment changes, resource detail pages redirect to list pages (resources don't exist across environments)

See [environment.store.svelte.ts](frontend/src/lib/stores/environment.store.svelte.ts) for implementation.

## Background Job Scheduling

Jobs use cron-based scheduling via `robfig/cron/v3`. To add a new job:

1. Implement the `Job` interface in `backend/pkg/scheduler/`:

```go
type Job interface {
    Name() string                      // Unique job identifier
    Schedule(ctx context.Context) string  // Cron expression (6-field with seconds)
    Run(ctx context.Context)           // Job execution logic
}
```

2. Create job with service dependencies:

```go
type MyJob struct {
    myService      *services.MyService
    settingsService *services.SettingsService
}

func NewMyJob(myService *services.MyService, settings *services.SettingsService) *MyJob {
    return &MyJob{myService: myService, settingsService: settings}
}

func (j *MyJob) Schedule(ctx context.Context) string {
    return j.settingsService.GetStringSetting(ctx, "myJobInterval", "0 0 * * * *") // hourly default
}
```

3. Register in [jobs_bootstrap.go](backend/internal/bootstrap/jobs_bootstrap.go):

```go
myJob := pkg_scheduler.NewMyJob(appServices.MyService, appServices.Settings)
newScheduler.RegisterJob(myJob)
```

**Note:** Cron uses 6 fields (with seconds): `"0 0 * * * *"` = every hour at :00:00

## Database Patterns (GORM)

### BaseModel

All models embed `BaseModel` for UUID primary key and timestamps:

```go
type MyModel struct {
    models.BaseModel           // ID, CreatedAt, UpdatedAt
    Name        string         `json:"name" gorm:"column:name" sortable:"true"`
    ForeignID   string         `json:"foreignId" gorm:"column:foreign_id"`
    Related     *OtherModel    `json:"related,omitempty" gorm:"foreignKey:ForeignID"`
}

func (MyModel) TableName() string { return "my_models" }
```

### Relationships & Preloading

Always use `Preload` for eager loading relationships:

```go
// Single preload
s.db.WithContext(ctx).Preload("Registry").Where("id = ?", id).First(&template)

// Multiple preloads
s.db.WithContext(ctx).
    Preload("Repository").
    Preload("Project").
    Where("id = ?", id).First(&sync)
```

### Custom Types

Use `models.JSON` for arbitrary JSON fields and `models.StringSlice` for string arrays — both implement `driver.Valuer` and `sql.Scanner`.

## Edge Agent Mode

Edge agents connect to a central Arcane manager via WebSocket tunnel, allowing management of Docker hosts behind NAT/firewalls.

**Architecture:**
- Manager: Receives tunnel connections, proxies HTTP requests over WebSocket
- Agent: Connects outbound to manager, executes requests locally

**Configuration** (agent side):
```bash
ARCANE_EDGE_AGENT=true
ARCANE_MANAGER_API_URL=https://manager.example.com
ARCANE_AGENT_TOKEN=<api-key>
```

**Message types** (see [tunnel.go](backend/internal/utils/edge/tunnel.go)):
- `request` / `response`: HTTP request/response proxying
- `heartbeat` / `heartbeat_ack`: Connection keepalive
- `ws_start` / `ws_data` / `ws_close`: WebSocket streaming (logs, stats)

**When implementing agent features:**
- Check `cfg.AgentMode` to skip manager-only logic (e.g., environment health checks)
- Agent auto-pairs with manager on startup if token is configured
- Edge connections are stateless — each request is independent
