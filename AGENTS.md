# Arcane Agent Guide

All AI-assisted work must follow [AI_POLICY.md](./AI_POLICY.md). Keep changes focused,
verify them locally, and disclose AI assistance when contributing.

Arcane is a Docker management platform with a Go backend, SvelteKit frontend,
headless agent modes, and a Cobra CLI.

## Non-negotiable rules

- Update existing code in place. Search before adding functions, services, wrappers,
  API clients, components, or utilities.
- Never add stubs, shims, pass-through helpers, or duplicate implementations.
- Call existing `pkg/` helpers directly instead of wrapping them.
- Add new code only for genuinely new behavior and integrate it with the owning domain.
- Keep comments short. If code needs a paragraph to explain its structure, simplify it.
- Never run state-changing Git commands. Do not stage, commit, push, tag, stash,
  create branches, or create worktrees.
- Always use the `golang-master` skill when writing Go.
- Name every unexported Go function with an `Internal` suffix.
- Put public/shared Go types in the top-level `types/` module.
- Put reusable helper utilities under `backend/pkg/utils/` in the appropriate package.
- After any change, run `just format all`, then `just lint all`, and fix every issue.
  Never revert formatter output.

## Repository architecture

The Go workspace contains three modules:

```text
backend/   Go application, HTTP API, domain logic, jobs, and embedded frontend
cli/       Cobra CLI and its API client
types/     Public domain and API contracts shared by backend and CLI
```

The frontend lives in `frontend/`. End-to-end tests live in `tests/`.

### Backend

The backend uses domain-oriented vertical slices:

```text
backend/
├── cmd/                 process entrypoint
├── api/                 API assembly and exceptional HTTP/stream/WebSocket routes
├── internal/
│   ├── <domain>/        domain module, handler, service, and related files
│   ├── bootstrap/       application lifecycle, router, jobs, and startup wiring
│   ├── di/              Fx dependency graph and providers
│   ├── config/          environment configuration
│   ├── database/        database setup and migrations
│   ├── middleware/      authentication, authorization, and environment proxying
│   └── models/          private GORM persistence models
├── pkg/                 reusable infrastructure and domain-independent libraries
├── resources/           migrations, email templates, and runtime assets
└── frontend/            embedded frontend build
```

Most domains under `backend/internal/<domain>/` follow this shape:

- `module.go` constructs the domain and exposes `Service()` or `Handler()` only when
  another domain needs them.
- `handler.go` owns Huma input/output types, route registration, and thin handlers.
- `service.go` and focused sibling files own business logic.
- Tests stay beside the code they cover.

Wire dependencies in `internal/di`; keep startup order and lifecycle hooks in
`internal/bootstrap`. Do not turn `api/` back into a global handlers directory or
recreate a global `internal/services` layer.

Use Echo v5 as the router and Huma v2 for typed REST/OpenAPI operations. Register
permissioned endpoints with `middleware.RegisterWithPermission`. Direct Echo routes
are reserved for WebSockets, streams, diagnostics, webhooks, Playwright support,
the environment proxy, and embedded frontend delivery.

Handlers translate typed HTTP data and call services. They do not contain business
logic. Services receive dependencies through constructors/Fx. Use `slog` for
structured logging and the existing `emperror.dev/errors` and `internal/common`
patterns for wrapped or semantic errors.

Before adding backend logic, search the owning domain plus:

- `backend/pkg/dockerutil` for Docker names, labels, clients, logs, and stream helpers.
- `backend/pkg/projects` for Compose parsing, discovery, and image references.
- `backend/pkg/pagination` for in-memory and database pagination.
- `backend/pkg/libarcane` for reusable Arcane engines and transport behavior.
- `backend/pkg/utils` for shared infrastructure utilities.

Persistence models embed `models.BaseModel`. Use existing GORM model helpers and
`Preload` relationships where appropriate; do not expose persistence models as API
contracts.

### Frontend

The frontend is SvelteKit v3 on Svelte 5. Configuration lives in
`frontend/vite.config.ts`.

```text
frontend/src/
├── routes/              SvelteKit pages and layouts
└── lib/
    ├── components/      shared UI components
    ├── config/          navigation and access-surface configuration
    ├── services/        API clients extending BaseAPIService
    ├── stores/          rune-based application state
    ├── types/           frontend-only TypeScript types
    └── utils/           frontend utilities
```

- Use Svelte 5 runes: `$props`, `$state`, `$derived`, and `$effect`.
- Do not use `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, or legacy slots.
- Extend `BaseAPIService`; reuse existing services and query/mutation patterns.
- Use precise TypeScript types. Do not introduce `any`.
- Reuse shared components before creating page-local variants.
- Put every rendered string behind Paraglide messages.
- Reuse a matching key from `frontend/messages/en.json` before adding one.
- Add new keys only to `en.json`; Crowdin manages every other locale.

### Multi-environment and authorization

- Environment ID `"0"` is the local Docker environment.
- Environment-scoped API paths use `/environments/{id}/...`.
- Await `environmentStore.ready` or `getCurrentEnvironmentId()` before requests.
- Redirect environment-specific detail pages when the selected environment changes.
- Backend permission middleware is authoritative; frontend gates are UX only.
- Keep the permission catalog, access-surface registry, and frontend navigation gates
  as separate layers.
- Determine global admin status from `PermissionSet.IsGlobalAdmin()` or the user DTO's
  `isGlobalAdmin`; never infer it from a role ID.

### Runtime modes and jobs

- Manager mode serves the UI and manages environments.
- Direct agent mode uses `AGENT_MODE=true` and accepts manager connections.
- Edge agent mode uses `EDGE_AGENT=true` with `MANAGER_API_URL` and dials the manager.
- Background jobs implement the scheduler job contract and are wired through
  `internal/di` and registered in `internal/bootstrap/jobs_bootstrap.go`.

## Validation

Use the narrowest relevant test first, then the repository gates:

```bash
just format all
just lint all
just test backend|cli|types|e2e|all
```

For AI-assisted code contributions, also run the development environment and manually
exercise the changed frontend and backend behavior as required by `AI_POLICY.md`.
