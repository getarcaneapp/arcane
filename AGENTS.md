# Arcane agent guide

Follow [AI_POLICY.md](./AI_POLICY.md), including its maintainer exemption. Outside
contributors must disclose AI assistance and meet its human verification requirements.

Arcane is a Docker management platform with a Go backend, SvelteKit frontend,
headless agent modes, and a Cobra CLI.

## Non-negotiable rules

- Search the owning domain and existing helpers before adding functions, services,
  API clients, components, or utilities. Update existing logic and its callers directly.
- Never add stubs, compatibility shims, pass-through wrappers, or duplicate
  implementations. Call existing helpers directly.
- Integrate new functionality with the owning domain. Keep each file focused on one
  coherent responsibility. Do not create god files in services, components, or CLI commands.
- Updating in place allows moving related logic into focused sibling files within the
  same domain. Split by responsibility, not a fixed line count. Avoid trivial file
  splits, unnecessary abstractions, and unrelated restructuring.
- Keep comments short. If code needs a paragraph to explain its structure, simplify it.
- Never run state-changing Git commands. Do not stage, commit, push, tag, stash,
  create branches, or create worktrees.
- Name every unexported Go function with an `Internal` suffix.
- Put public/shared Go types in the top-level `types/` module.
- Put reusable helper utilities under `backend/pkg/utils/` in the appropriate package.
- Add tests only for new functionality. For bug fixes and refactors, update existing
  tests when necessary and run relevant existing coverage; do not add regression tests.
- Never add handler tests. Test new business behavior at the service layer or in its
  owning logic package. Preserve existing handler tests; this rule does not authorize
  deleting them.
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

Domains under `backend/internal/<domain>/` own their behavior and routes:

- `module.go` wires the domain and registers its routes. Expose services or handlers
  only when collaborators need them, following the existing module's pattern.
- `handler.go` and focused route files translate typed HTTP input and call services.
- Business logic lives in service or domain-named files and focused siblings, such as
  the project domain's Compose cache, lifecycle, sync, and workspace files. There is
  no requirement to collect everything in `service.go`.
- Domain persistence models live beside their owning logic, commonly in `model.go`.
  Tests belong beside the logic they cover, subject to the test rules above.

Wire dependencies in `internal/di`; keep startup order, lifecycle hooks, database
migration wiring, and router assembly in `internal/bootstrap`. The remaining `api/`
code handles API assembly, diagnostics, streams, WebSockets, and webhook dispatch.
Keep ordinary REST endpoints in their domains. Do not expand `api/handlers` into a
central handler layer or recreate a global `internal/services` or models package.

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

Persistence models use `database.BaseModel` and existing database helpers where
appropriate. Reuse GORM relationships and `Preload`; keep persistence models separate
from public API contracts in `types/`.

### CLI and shared types

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
Keep commands focused on input, API calls, and output; backend business behavior
belongs in its owning backend domain. Keep shared contracts in `types/` independent
of backend persistence and application wiring.

### Frontend

The frontend is SvelteKit v3 on Svelte 5. Configuration lives in
`frontend/vite.config.ts`.

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

- Use Svelte 5 runes: `$props`, `$state`, `$derived`, and `$effect`.
- Do not use `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, or legacy slots.
- Extend `BaseAPIService`; reuse existing services and query/mutation patterns.
- Use precise TypeScript types. Do not introduce `any`.
- Reuse shared components before creating page-local variants.
- Put every rendered string behind Paraglide messages.
- Reuse a matching key from `frontend/messages/en.json` before adding one.
- Add new keys only to `en.json`; Crowdin manages every other locale.
- Generate Paraglide output through the existing tooling; do not edit it by hand.

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

Adding tests and running tests are separate decisions. The new-functionality-only
rule does not waive verification. Run the narrowest relevant existing coverage for
changed behavior, then select the appropriate repository test target. Examples:

```bash
just test backend
just test cli
just test types
```

Use `just test e2e` for browser coverage when the required environment is available.
`just test all` includes E2E tests and requires their prerequisites. Do not start a
development stack implicitly to satisfy a test target.

After every change, including documentation changes, run these gates in order:

```bash
just format all
just lint all
```

Fix reported issues and retain formatter output. Report what ran, what passed,
and any blockers. Documentation-only changes do not require adding tests.

For maintainer work, do not start, restart, or rebuild the development stack unless
the user explicitly asks. Outside contributions must satisfy the development and
human testing requirements in `AI_POLICY.md` before submission. Do not claim manual
verification that has not happened.
