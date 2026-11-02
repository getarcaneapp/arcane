# Arcane agent guide

Arcane is a Docker management platform.

- **Backend:** Go
- **Frontend:** SvelteKit
- **Also:** headless agent modes and a Cobra CLI

## Ground rules (every change)

- Follow [AI_POLICY.md](./AI_POLICY.md), including the maintainer exemption.
- Outside contributors: disclose AI use and meet the policy's dev and human verification rules.
- **Git:** never run state-changing commands.
  - No stage, commit, push, tag, stash, branch, or worktree.
- **Dev stack:** maintainers do not start, restart, or rebuild it unless the user asks.
- **Search first:** check the owning domain and existing helpers before adding functions, services, API clients, components, or utils.
  - Update existing logic and its callers directly.
- **No clutter:** no stubs, compatibility shims, pass-through wrappers, or duplicates. Call existing helpers directly.
- **Tests:**
  - New functionality gets tests.
  - Bug fixes and refactors: update existing tests if needed, then run relevant coverage.
  - No regression tests.
  - Never add handler tests. Test business behavior at the service layer or its owning logic package.
  - Keep existing handler tests.
- **Checks:** after every change, including docs, run `just format all`, then `just lint all`.
  - Fix every issue. Keep the formatter's output.
- **Reporting:** state what you verified and any blockers.
  - Never claim manual verification that did not happen.

## Keep changes focused

- **Ownership:** new features go in the domain that owns them.
- **One job per file:** no god files in services, components, or CLI commands.
- **File layout:** use the standard domain file set.
  - Substantial child features go in `children/<feature>/`.
  - Split by responsibility, not line count.
- **Avoid:** trivial packages, needless abstractions, unrelated restructuring.
- **Comments:** keep them short. If the structure needs a paragraph, simplify the code.
- **Ternaries:** no nested or chained ones. Use `if`/`else` or `switch`.

### Go imports and shared code

- **Imports:** default names. Alias only to fix a real conflict.
- **Shared contracts:** top-level `types/`. Persistence models stay in their domain.
- **Helpers:** a purpose-named package directly under `backend/pkg/`.

### Go style

- **Literals:** one keyed literal per value. Never assign fields after construction.
- **Defaults:** `cmp.Or`, not `if x == "" { x = … }`.
- **Pointers:** `kit.FromPtr` or `new(v)`. No `p != nil && *p` chains.
- **Injected deps:** no nil checks. Tests pass fakes.
- **No wrappers:** no forwarding wrappers or one-line helpers.
- **Params:** no bool flags or long param lists on exported funcs. Use an options struct.
- **Constructors:** 5+ deps means an `fx.In` params struct.
- **Slices and maps:** no nil-to-empty guards. json v2 encodes them as `[]` and `{}`.

## Backend

Organized by domain. Each domain owns its behavior and routes.

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

Under `backend/internal/<domain>/`:

- `module.go`: composition and route registration
- `service.go`: business operations and orchestration
- `model.go`: persistence models
- `handler.go`: HTTP input and output
- `helpers.go`: package-local helpers with a clear purpose

- Create a file only when it has a real responsibility.
- No placeholders.

### Child features

- **Location:** `<domain>/children/<feature>/`, same file set.
- **`children/` itself:** no Go files.
- **Imports:** only the immediate parent imports a child.
  - Siblings, ancestors, other domains, DI, bootstrap, and API use the parent's API.
  - This rule applies recursively.
- **Children never import their parent.**
- **Parent role:** coordinates children and keeps child instances private.
- **Exposure:** never expose child types in parent signatures or inject them outside.
- **Examples:**
  - Image patching: `image/children/patch`
  - System recovery backups: `system/children/backup`, with snapshots and system-managed volumes beneath it
- **Shared models:** persistence models go in the parent's `model.go`. Shared contracts go in top-level `types/`.

### Filenames and test pairing

- **Names:** single words, no underscores.
- **Build tags:** may append the tag. Examples: `service_playwright.go`, `service_buildables.go`, `helpers_unix.go`.
- **`!unix` implementation:** `helpers_nonunix.go`.
- **Tests:** beside the production file, using the exact basename plus `_test.go`.

```text
service.go              → service_test.go
service_playwright.go   → service_playwright_test.go
```

- Never create a production file just to justify a test name.
- **Scope:** business domains and their children.
- **Excluded:** `bootstrap`, `common`, `config`, `database`, `di`, `scheduler`, `backend/pkg`, CLI packages, shared types.
- **Middleware:** descriptive single-word names like `cors.go`, `csrf.go`, `environment.go`, with the same test pairing.

### Wiring and API assembly

- **DI:** wire dependencies in `internal/di`.
- **Bootstrap:** startup order, lifecycle hooks, migration wiring, and router assembly live in `internal/bootstrap`.
- **`api/`:** API assembly, diagnostics, streams, WebSockets, webhook dispatch.
- **REST:** ordinary endpoints go in their domains.
- **Don't:** grow `api/handlers` into a central layer, or recreate a global `internal/services` or models package.

### HTTP and business logic

- **Router:** Echo v5.
- **Typed REST/OpenAPI:** Huma v2.
- **Permissions:** register permissioned endpoints with `middleware.RegisterWithPermission`.
- **Direct Echo routes, only for:**
  - WebSockets and streams
  - Diagnostics and webhooks
  - Playwright support
  - The environment proxy
  - Embedded frontend delivery
- **Handlers:** translate HTTP data and call services. No business logic.
- **Services:** get dependencies through constructors or Fx.
- **Use:**
  - `slog` for structured logging
  - Standard `errors` and `fmt.Errorf("…: %w")` for wrapping
  - `internal/common.Classify` for semantic errors
  - `types/base.FieldError` for validation fields

### Existing helpers

Search these before adding backend logic:

- `kit.getarcane.app/docker` (Kit): Docker names, labels, clients, logs, streams
- `backend/pkg/projects`: Compose parsing, discovery, image refs
- `backend/pkg/pagination`: in-memory and database pagination
- `backend/pkg/agent`, `backend/pkg/ws`: agent tunnel and WebSocket transport
- `backend/pkg/httpx`, `backend/pkg/handlerutil`: outbound HTTP and handler helpers
- `backend/pkg/registry`: registry auth, hosts, and rate limiting
- `backend/pkg/volumes`: volume helper containers and renames
- `backend/pkg/*`: one package per purpose; check for an existing one before adding

### Persistence

- **Models:** use `database.BaseModel` and DB helpers.
- **Relations:** reuse GORM relationships and `Preload`.
- **Separation:** keep persistence models apart from `types/` API contracts.

### Migrations

- **Location:** Goose Go migrations in `backend/internal/database/migrations`.
- **Baseline:** version 94 is Arcane v2.17.1, loaded from `baseline/{sqlite,postgres}.sql`.
  - Older databases upgrade to v2.17.1 first.
- **Naming:** `NNNNN_name.go`. Goose reads the version from the filename.
- **Registration:** in `init()` with `goose.AddMigrationContext(run(…), run(…))`.
- **SQL:**
  - Raw SQL only. Never import GORM, domain packages, or `types/`. Lint enforces this.
  - Keep SQL in top-level constants.
  - `run(sqlite, postgres)` picks the dialect. Pass the same constant twice when they match.
- **Transactions:** use `goose.AddMigrationNoTxContext` only for work that can't run in a transaction, like SQLite rebuilds with `PRAGMA foreign_keys=OFF`.
- **Down:** provide a real `down`, unless the change is an irreversible data cleanup.
- **Tests:** add an up/down test in `backend/internal/database/database_test.go`.

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

- **Commands:** update existing ones. Reuse the CLI client, config, and output code.
- **Scope:** commands handle input, API calls, and output only.
- **Business logic:** lives in its owning backend domain.
- **`types/`:** stays independent of backend persistence and wiring.

## Frontend

- **Stack:** SvelteKit v3, Svelte 5.
- **Config:** `frontend/vite.config.ts`.

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

- **Runes:** use `$props`, `$state`, `$derived`, `$effect`.
- **Banned:** `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, legacy slots.
- **Services:** extend `BaseAPIService`. Reuse existing services and query/mutation patterns.
- **Types:** precise TypeScript. No `any`.
- **Components:** reuse shared ones before making page-local variants.

### Error handling

Don't stop at `console.error`. Pick the matching path:

- **One-shot actions:** `handleApiResultWithCallbacks`.
- **No `Result` fit** (e.g. TanStack `onError`): `toast.error(headline, { description: extractApiErrorMessage(err) })`.
- **Streams and polling:** show an inline unavailable state where the data renders.
- **Page loads:** rethrow through `throwPageLoadError`.
- **Messages:** `APIError.message` already holds the server message. Show it, not a canned string.

### Translations

- **Strings:** every rendered string goes through Paraglide.
- **Reuse:** check `frontend/messages/en.json` for a matching key first.
- **New keys:** add only to `en.json`. Crowdin handles other locales.
- **Naming:** snake_case from the English text, no domain prefix.
  - `"apply": "Apply"`
  - `"view_details": "View Details"`
  - Full sentences get a short descriptive key, like `"engine_info_description": "Detailed information about your container installation"`.
- **Placement:** append new keys at the end. Existing keys keep their names until touched.
- **Generated code:** run the existing tooling. Never hand-edit it.

## Environments and authorization

### Environment-scoped behavior

- **Local Docker:** environment ID `"0"`.
- **Paths:** `/environments/{id}/...`.
- **Before requests:** await `environmentStore.ready` or `getCurrentEnvironmentId()`.
- **Switching:** redirect environment-specific detail pages when the environment changes.

### Permissions

- **Authority:** backend permission middleware. Frontend gates are UX only.
- **Layers:** keep these separate:
  - Permission catalog
  - Access-surface registry
  - Frontend navigation gates
- **Global admin:** use `PermissionSet.IsGlobalAdmin()` or the user DTO's `isGlobalAdmin`.
  - Never infer it from a role ID.

## Runtime modes and jobs

- Manager mode serves the UI and manages environments.
- Edge agent mode uses `ARCANE_EDGE_AGENT=true` with `ARCANE_MANAGER_API_URL` and dials the manager.

- **Modes:**
  - Manager: serves the UI and manages environments.
  - Edge agent: `ARCANE_EDGE_AGENT=true` with `ARCANE_MANAGER_API_URL`. Dials the manager.

### Background jobs

- **Contract:** implement the scheduler job contract.
- **Wiring:** `internal/di`, then register in `internal/bootstrap/jobs_bootstrap.go`.
- **Multi-step, fan-out, or resumable jobs:** use `backend/internal/scheduler/flow` (Francis workflows in coordinator runs).
  - Follow the migration recipe in the package doc.
  - Steps are idempotent service methods.
  - The last step returns `scheduler.Outcome`.
  - The job registers a `flow.Job`.
  - `flowtest.AssertDefinitions` guards version bumps.
- **Single-operation jobs:** keep them ordinary.

## Validation

### Tests

- Adding tests and running tests are separate. Verify every change anyway.
- Run the narrowest relevant existing coverage first.
- Then pick a repo target:

```bash
just test backend
just test cli
just test types
```

- `just test e2e`: browser coverage, when the environment is ready.
- `just test all`: includes E2E and needs its prerequisites.
- Never start a dev stack just to satisfy a test target.
- Docs-only changes need no new tests.

### Required checks

After every change, including docs, run in order:

```bash
just format all
just lint all
```

- Fix every reported issue.
- Keep the formatter's output.

### Reporting

- Say what ran, what passed, and what you could not verify.
- Outside contributors must also meet the dev and human testing rules in `AI_POLICY.md` before submitting.
