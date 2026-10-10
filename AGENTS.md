# Arcane agent guide

Arcane is a Docker management platform with a Go backend, SvelteKit frontend, headless agent modes, and a Cobra CLI.

## Rules that apply to every change

Follow [AI_POLICY.md](./AI_POLICY.md), including its maintainer exemption. Outside contributors must disclose AI assistance and meet the policy's development and human verification requirements.

- Never run state-changing Git commands. Do not stage, commit, push, tag, stash, create branches, or create worktrees.
- For maintainer work, do not start, restart, or rebuild the development stack unless the user explicitly asks.
- Search the owning domain and existing helpers before adding functions, services, API clients, components, or utilities. Update existing logic and its callers directly.
- Do not add stubs, compatibility shims, pass-through wrappers, or duplicate implementations. Call existing helpers directly.
- Add tests only for new functionality. For bug fixes and refactors, update existing tests when needed and run relevant existing coverage. Do not add regression tests.
- Never add handler tests. Test new business behavior at the service layer or in its owning logic package. Preserve existing handler tests.
- After every change, including documentation changes, run `just format all`, then `just lint all`. Fix every issue and keep the formatter's output.
- Report what you verified and any blockers. Never claim manual verification that did not happen.

## Keep changes focused

New functionality belongs in the domain that owns it. Keep each file focused on one clear responsibility. Do not create god files in services, components, or CLI commands.

Use the standard domain file set and move substantial child features into `children/<feature>/`. Split code by responsibility rather than a fixed line count. Avoid trivial packages, unnecessary abstractions, and unrelated restructuring.

Keep comments short. If the structure needs a paragraph to explain it, simplify the code.

Avoid nested or chained ternary expressions unless absolutely necessary. Prefer `if`/`else` or `switch` for conditional logic with multiple branches.

### Go imports and shared code

- Use default Go import names. Add an alias only to resolve an actual naming conflict.
- Put public and shared Go contracts in the top-level `types/` module. Keep persistence models in their owning domain.
- Put reusable helper utilities in the appropriate package under `backend/pkg/utils/`.

## Repository layout

The Go workspace contains three modules:

```text
backend/   Go application, HTTP API, domain logic, jobs, and embedded frontend
cli/       Cobra CLI and its API client
types/     Public domain and API contracts shared by backend and CLI
```

The frontend lives in `frontend/`. End-to-end tests live in `tests/`.

## Backend

The backend is organized by domain. Each domain owns its behavior and routes.

```text
backend/
├── cmd/                 process entrypoint
├── api/                 API assembly and exceptional HTTP/stream/WebSocket routes
├── internal/
│   ├── <domain>/        module, routes, business logic, and persistence models
│   ├── bootstrap/       application lifecycle, router, jobs, and startup wiring
│   ├── di/              Fx dependency graph and providers
│   ├── config/          environment configuration
│   ├── database/        database setup and shared persistence primitives
│   └── middleware/      authentication, authorization, and environment proxying
├── pkg/                 reusable engines, infrastructure, and helpers
├── resources/           migrations, email templates, and runtime assets
└── frontend/            embedded frontend build
```

### Domain files

Use these files under `backend/internal/<domain>/`:

- `module.go`: composition and route registration
- `service.go`: business operations and orchestration
- `model.go`: persistence models
- `handler.go`: HTTP input and output
- `helpers.go`: package-local helpers with a clear purpose

Create a file only when it has a real responsibility. Do not add placeholders.

### Child features and import boundaries

Place substantial features under `<domain>/children/<feature>/` and use the same standard file set. The `children/` directory itself must contain no Go files.

Only a child package's immediate parent may import it. Siblings, higher-level ancestors, other domains, DI, bootstrap, and API assembly must use the parent's API. Apply this boundary recursively.

Children must not import their parent. The parent coordinates interactions between children and keeps child instances private. Image patching belongs under `image/children/patch`. System recovery backups belong under `system/children/backup`, with snapshots and system-managed volumes beneath that feature.

Do not expose child types through parent API signatures or inject child instances into outside packages. Keep shared persistence models in the parent's `model.go` and shared contracts in the top-level `types/` module.

### Filenames and test pairing

Production filenames must be single words without underscores. Build-tagged files may append their tag:

```text
service_playwright.go
service_buildables.go
helpers_unix.go
```

Use `helpers_nonunix.go` for a `!unix` implementation.

Tests belong beside the production file they cover. Use the exact production basename followed by `_test.go`:

```text
service.go              → service_test.go
service_playwright.go   → service_playwright_test.go
```

Never create a production file just to justify a test filename.

These layout rules apply to business domains and their children. The filename policy excludes:

- Infrastructure packages: `bootstrap`, `common`, `config`, `database`, and `di`
- `backend/pkg`
- CLI packages
- Shared types

Middleware uses descriptive, single-word production filenames such as `cors.go`, `csrf.go`, and `environment.go`, with the same exact test-file pairing.

### Wiring and API assembly

Wire dependencies in `internal/di`.

Keep startup order, lifecycle hooks, database migration wiring, and router assembly in `internal/bootstrap`.

The remaining `api/` code handles API assembly, diagnostics, streams, WebSockets, and webhook dispatch. Ordinary REST endpoints belong in their domains. Do not expand `api/handlers` into a central handler layer or recreate a global `internal/services` or models package.

### HTTP and business logic

Use Echo v5 as the router and Huma v2 for typed REST/OpenAPI operations. Register permissioned endpoints with `middleware.RegisterWithPermission`.

Direct Echo routes are reserved for:

- WebSockets and streams
- Diagnostics and webhooks
- Playwright support
- The environment proxy
- Embedded frontend delivery

Handlers translate typed HTTP data and call services. They must not contain business logic. Services receive dependencies through constructors/Fx.

Use:

- `slog` for structured logging
- Standard `errors` and `fmt.Errorf("…: %w")` for error handling and wrapping
- `internal/common.Classify` for semantic errors
- `types/base.FieldError` for validation fields

### Existing helpers

Before adding backend logic, search the owning domain and the relevant shared packages:

- `go.getarcane.app/docker` (Kit): Docker names, labels, clients, logs, and stream helpers
- `backend/pkg/projects`: Compose parsing, discovery, and image references
- `backend/pkg/pagination`: in-memory and database pagination
- `backend/pkg/libarcane`: reusable Arcane engines and transport behavior
- `backend/pkg/utils`: shared infrastructure utilities

### Persistence

Use `database.BaseModel` and existing database helpers where appropriate. Reuse GORM relationships and `Preload`.

Keep persistence models separate from the public API contracts in `types/`.

## CLI and shared types

```text
cli/
├── main.go              CLI entrypoint
├── pkg/                 Cobra root and domain command packages
└── internal/
    ├── client/          shared API client
    ├── config/          CLI configuration
    ├── cmdutil/         command support
    ├── output/          output rendering
    └── ...              prompts, runtime state, and other internal support

types/                   shared domain and API contracts, grouped by domain
```

Update existing commands and reuse the CLI client, configuration, and output code.

Keep commands focused on input, API calls, and output. Backend business behavior belongs in its owning backend domain.

Shared contracts in `types/` must remain independent of backend persistence and application wiring.

## Frontend

The frontend uses SvelteKit v3 and Svelte 5. Configuration lives in `frontend/vite.config.ts`.

```text
frontend/src/
├── routes/              SvelteKit pages and layouts
└── lib/
    ├── components/      shared UI components
    ├── config/          navigation and access-surface configuration
    ├── hooks/           reusable reactive behavior
    ├── layouts/         shared layout components
    ├── query/           shared query keys
    ├── paraglide/       generated message code
    ├── services/        API clients extending BaseAPIService
    ├── stores/          rune-based application state
    ├── types/           frontend-only TypeScript types
    └── utils/           frontend utilities
```

### Components and state

- Use Svelte 5 runes: `$props`, `$state`, `$derived`, and `$effect`.
- Do not use `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, or legacy slots.
- Extend `BaseAPIService` and reuse existing services and query/mutation patterns.
- Use precise TypeScript types. Do not introduce `any`.
- Reuse shared components before creating page-local variants.

### Error handling

Component error paths must do more than call `console.error`:

- For one-shot actions, use `handleApiResultWithCallbacks`.
- Where a `Result` does not fit, such as TanStack `onError`, use `toast.error(headline, { description: extractApiErrorMessage(err) })`.
- For streams and polling feeds, show an inline unavailable state where the data renders.
- For page loads, rethrow through `throwPageLoadError`.

A thrown `APIError.message` already contains the server's message. Surface that message rather than showing only a canned string.

### Translations

- Put every rendered string behind Paraglide messages.
- Reuse a matching key from `frontend/messages/en.json` before adding one.
- Add new keys only to `en.json`. Crowdin manages every other locale.
- Generate Paraglide output through the existing tooling. Do not edit generated files by hand.

## Environments and authorization

### Environment-scoped behavior

- Environment ID `"0"` is the local Docker environment.
- Environment-scoped API paths use `/environments/{id}/...`.
- Await `environmentStore.ready` or `getCurrentEnvironmentId()` before making requests.
- Redirect environment-specific detail pages when the selected environment changes.

### Permissions

Backend permission middleware is authoritative. Frontend gates are for user experience only.

Keep the permission catalog, access-surface registry, and frontend navigation gates as separate layers.

Determine global admin status through `PermissionSet.IsGlobalAdmin()` or the user DTO's `isGlobalAdmin`. Never infer it from a role ID.

## Runtime modes and jobs

- Manager mode serves the UI and manages environments.
- Direct agent mode uses `AGENT_MODE=true` and accepts manager connections.
- Edge agent mode uses `EDGE_AGENT=true` with `MANAGER_API_URL` and dials the manager.

Background jobs implement the scheduler job contract. Wire them through `internal/di` and register them in `internal/bootstrap/jobs_bootstrap.go`.

Multi-step, fan-out, or resumable jobs use `backend/pkg/scheduler/flow`: Francis workflows that run inside coordinator runs. Follow the migration recipe in its package doc. Steps are idempotent service methods, the last step returns `scheduler.Outcome`, the job registers a `flow.Job`, and `flowtest.AssertDefinitions` guards version bumps. Keep single-operation jobs as ordinary jobs.

## Validation

### Tests

Adding tests and running tests are separate decisions. The new-functionality-only rule does not remove the need to verify changes.

Run the narrowest relevant existing coverage for the changed behavior, then choose the appropriate repository test target:

```bash
just test backend
just test cli
just test types
```

Use `just test e2e` for browser coverage when the required environment is available. `just test all` includes E2E tests and requires their prerequisites.

Do not start a development stack implicitly to satisfy a test target. Documentation-only changes do not require adding tests.

### Required checks

After every change, including documentation changes, run these commands in order:

```bash
just format all
just lint all
```

Fix every reported issue and retain the formatter's output.

Report what ran, what passed, and anything you could not verify. Outside contributions must also meet the development and human testing requirements in `AI_POLICY.md` before submission.
