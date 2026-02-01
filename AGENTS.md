# Arcane AI Agent Instructions

> **All AI agents must conform to [AI_POLICY.md](./AI_POLICY.md)**

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

### Anti-Pattern 1: Business Logic in Handlers
**Bad**: Handler contains business logic
```go
func (h *ContainerHandler) RestartContainer(ctx context.Context, input *RestartInput) (*RestartOutput, error) {
    container, err := h.dockerClient.ContainerInspect(ctx, input.ID)
    if container.State.Running {
        h.dockerClient.ContainerStop(ctx, input.ID, nil)
    }
    return h.dockerClient.ContainerStart(ctx, input.ID, nil)
}
```

**Good**: Handler calls service
```go
func (h *ContainerHandler) RestartContainer(ctx context.Context, input *RestartInput) (*RestartOutput, error) {
    err := h.containerService.Restart(ctx, input.EnvironmentID, input.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to restart container: %w", err)
    }
    return &RestartOutput{Success: true}, nil
}
```

### Anti-Pattern 2: Svelte 4 Syntax
**Bad**: Using deprecated Svelte 4 patterns
```svelte
<script>
  export let name;
  $: greeting = `Hello ${name}`;
</script>
<button on:click={handleClick}>Click</button>
```

**Good**: Using Svelte 5 runes
```svelte
<script lang="ts">
  let { name }: { name: string } = $props();
  let greeting = $derived(`Hello ${name}`);
</script>
<button onclick={handleClick}>Click</button>
```

### Anti-Pattern 3: Missing Multi-Environment Support
**Bad**: Hardcoded API path without environment
```typescript
async getContainers() {
    return this.api.get('/containers');
}
```

**Good**: Include environment ID in path
```typescript
async getContainers() {
    const envId = await environmentStore.getCurrentEnvironmentId();
    return this.api.get(`/environments/${envId}/containers`);
}
```

### Anti-Pattern 4: Missing BaseModel
**Bad**: Model without standard fields
```go
type Stack struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

**Good**: Model with BaseModel
```go
type Stack struct {
    models.BaseModel
    Name string `json:"name" gorm:"column:name"`
}

func (Stack) TableName() string { return "stacks" }
```

### Anti-Pattern 5: Using TypeScript `any`
**Bad**: Untyped data
```typescript
function processContainer(data: any) {
    return data.name;
}
```

**Good**: Properly typed
```typescript
import type { Container } from '$lib/types';

function processContainer(data: Container): string {
    return data.name;
}
```

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

## AI-Assisted Contributions

If you're an AI coding agent (like Claude Code, GitHub Copilot, Cursor, or similar) assisting a human developer:

### Required Reading

1. **Must read**: [AI_POLICY.md](AI_POLICY.md) — Disclosure requirements and quality standards
2. **Must follow**: All coding patterns in this document
3. **Must ensure**: Human has tested the changes locally

### Common AI Pitfalls to Avoid

When working with Arcane:

❌ **Don't use Svelte 4 syntax**: This project uses Svelte 5 exclusively. No `export let`, no `on:click`, no `$:` reactive statements.

❌ **Don't put business logic in handlers**: Handlers should be thin wrappers that call services. Check `backend/internal/services/` for patterns.

❌ **Don't ignore multi-environment patterns**: All API endpoints must include environment ID. Check `environmentStore` usage in frontend services.

❌ **Don't skip BaseModel**: All database models must embed `models.BaseModel` for UUID and timestamps.

❌ **Don't ignore existing patterns**: Before writing new code, search for similar functionality:
```bash
# Find existing patterns
git grep "func.*Service" backend/internal/services/
git grep "extends BaseAPIService" frontend/src/lib/services/
```

### Good AI-Assisted Contribution Pattern

1. **Start with the issue**: Read the full GitHub issue and understand the user's actual problem
2. **Find existing patterns**: Search for similar code in the same package/directory
3. **Follow the pattern**: Match structure, error handling, naming conventions
4. **Test comprehensively**: Run dev environment, verify frontend and backend work
5. **Explain in human terms**: Write PR descriptions that explain WHY, not just WHAT

### Testing Requirements

Before submitting any AI-assisted contribution, ensure:

```bash
# 1. Start development environment
./scripts/development/dev.sh start

# 2. Backend tests (if you changed Go code)
cd backend && go test ./...

# 3. Frontend type checking (if you changed frontend code)
pnpm -C frontend check:ci

# 4. E2E tests
pnpm test

# 5. Verify hot reload works
# - Frontend: http://localhost:3000
# - Backend: http://localhost:3552
```

If any of these fail, **do not submit the PR**. Fix the issues first.

### PR Description Template for AI-Assisted Contributions

```markdown
## Summary
[One paragraph explaining what this PR does and why]

## Related Issue
Fixes #[issue number]

## Changes
- [Specific change 1 with rationale]
- [Specific change 2 with rationale]

## Testing
- [ ] Dev environment starts successfully
- [ ] Backend tests pass: `cd backend && go test ./...`
- [ ] Frontend type checks pass: `pnpm -C frontend check:ci`
- [ ] Manually tested: [describe how]

## AI Tool Used
AI Tool: [e.g., Claude Code, GitHub Copilot, Cursor]
Assistance Level: [Significant/Moderate/Minor]
```
