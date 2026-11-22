# Arcane

Arcane is a container management platform primarily supporting Docker and Podman. The goal of this project is for everyone's features to be implemented regardless of skill level, while sticking to the specifications of each container runtime.

## Ground rules

- Follow [AI_POLICY.md](AI_POLICY.md), including the maintainer exemption. Outside contributors must disclose AI use and meet its dev and human verification rules.
- Never run state-changing git commands: no stage, commit, push, tag, stash, branch, or worktree.
- Do not start, restart, or rebuild the dev stack unless the user asks.
- Search the owning domain and existing helpers before adding functions, services, API clients, components, or utils. Update existing logic and its callers directly.
- No stubs, compatibility shims, pass-through wrappers, or duplicates. Call existing helpers directly.

## Fork specifics

This is the viogus fork. `main` tracks upstream 3.1.x and `2.x` is the frozen 2.17.x maintenance line. Fork features live
in the v3 line: docker-host environments (the
manager serves a Docker API endpoint in-process through `pkg/envcontext`), volume backup history
and policies scoped by `environment_id`, per-environment daemon event streams, and the
`ARCANE_BACKUPS_DISABLED` kill switch. Images are published by `.github/workflows/publish.yml` as
lean `scratch` builds (`runner-lean` / `agent-lean` targets) and labelled `<version>+cdf.<n>`.
See [FORK.md](./FORK.md).

## Where code lives

## Backend

- Domain packages under `backend/internal/<domain>/` use `module.go` (composition and route registration), `service.go` (business operations and orchestration), `model.go` (persistence models), `handler.go` (HTTP input and output), and `helpers.go` (package-local helpers). Create a file only when it has a real responsibility; no placeholders.
- Substantial child features live in `<domain>/children/<feature>/` using the same file set (for example, `image/children/patch` or `system/children/backup`), with no Go files in `children/` itself. Only the immediate parent imports a child (recursively); children never import their parent. Parents coordinate children, keep child instances private, and never expose child types in signatures or inject them outside. Shared persistence models go in the parent's `model.go`; shared contracts go in `types/`.
- Filenames in business domains, their children, and `middleware/` (`cors.go`, `csrf.go`, `environment.go`) are single words with no underscores, except build tags (`service_playwright.go`, `service_buildables.go`, `helpers_unix.go`, `helpers_nonunix.go`). Tests sit beside the production file with the exact basename plus `_test.go` (`service.go` -> `service_test.go`). Never create a production file just to justify a test name. Excluded: `bootstrap`, `common`, `config`, `database`, `di`, `scheduler`, `backend/pkg`, CLI packages, and shared types.
- Echo v5 is the router and Huma v2 handles typed REST/OpenAPI endpoints. Register permissioned endpoints with `middleware.RegisterWithPermission`. Use direct Echo routes only for WebSockets (including diagnostics streams), the agent tunnel connection, the environment proxy, and embedded frontend delivery; everything else goes through Huma, including webhooks, pprof, and Playwright support.
- Handlers translate HTTP data and call services with no business logic. Services get dependencies through constructors or Fx.
- Use context-aware `slog` calls (`InfoContext`, `WarnContext`, `ErrorContext`) with static messages and camelCase key-value pairs, standard `errors` with `fmt.Errorf("…: %w")` for wrapping, `internal/common.Classify` for semantic errors, and `types/base.FieldError` for validation fields.
- Persistence models use `database.BaseModel` and DB helpers, reuse GORM relationships and `Preload`, and stay separate from `types/` API contracts.
- Migrations are Goose Go files in `backend/internal/database/migrations` named `NNNNN_name.go` (version 94 is the Arcane v2.17.1 baseline from `baseline/{sqlite,postgres}.sql`; older databases upgrade to v2.17.1 first). Register them in `init()` with `goose.AddMigrationContext(run(…), run(…))`, using `AddMigrationNoTxContext` only when work cannot run in a transaction (such as SQLite rebuilds with `PRAGMA foreign_keys=OFF`). Keep raw SQL in top-level constants—never import GORM, domain packages, or `types/` (enforced by lint)—and pass the same constant twice to `run(sqlite, postgres)` when dialects match. Provide a real `down` unless it is an irreversible data cleanup, and add an up/down test in `backend/internal/database/database_test.go`.
- Background jobs implement the scheduler job contract, are wired in `internal/di`, and are registered in `internal/bootstrap/jobs_bootstrap.go`. Keep single-operation jobs ordinary. For multi-step, fan-out, or resumable jobs, use `backend/internal/scheduler/flow` (Francis workflows in coordinator runs): follow the package doc migration recipe, make steps idempotent service methods, return `scheduler.Outcome` from the last step, register a `flow.Job`, and guard version bumps with `flowtest.AssertDefinitions`.

## Frontend

- Use Svelte 5 runes (`$props`, `$state`, `$derived`, `$effect`). Never use `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, or legacy slots.
- API clients extend `BaseAPIService`. Reuse existing services, shared components, and query/mutation patterns before making page-local variants. Use precise TypeScript; no `any`.
- Do not stop at `console.error`. Use `handleApiResultWithCallbacks` for one-shot actions, `toast.error(headline, { description: extractApiErrorMessage(err) })` when `Result` does not fit (such as TanStack `onError`), an inline unavailable state for streams and polling, and `throwPageLoadError` in page loads. `APIError.message` already holds the server message—show it, not a canned string.
- Every rendered string goes through Paraglide. Reuse a matching key in `frontend/messages/en.json` first. Append new keys at the end of `en.json` only (Crowdin handles other locales), using `snake_case` from the English text with no domain prefix (`"apply": "Apply"`, `"view_details": "View Details"`, or a short descriptive key for full sentences like `"engine_info_description"`). Existing keys keep their names until touched.

## Environments, permissions, and runtime modes

- Local Docker uses environment ID `"0"` and paths under `/agents/{id}/...`. Await `environmentStore.ready` or `getCurrentEnvironmentId()` before making requests, and redirect environment-specific detail pages when the environment changes.
- Backend permission middleware is the authority; frontend gates are UX only. Keep the permission catalog, access-surface registry, and frontend navigation gates separate. Check global admin via `PermissionSet.IsGlobalAdmin()` or the user DTO's `isGlobalAdmin`; never infer it from a role ID.
- Manager mode serves the UI and manages environments. Edge agent mode runs with `ARCANE_EDGE_AGENT=true` and `ARCANE_MANAGER_API_URL`, dialing the manager.

## Taste

- Put new features in their owning domain, keep one job per file (no god files in services, components, or CLI commands), split by responsibility rather than line count, and avoid trivial packages, needless abstractions, or unrelated restructuring.
- Keep comments short. If the structure needs a paragraph, simplify the code.
- No nested or chained ternaries; use `if`/`else` or `switch`.
- Always use keyed struct and composite literals: one keyed literal per value, never unkeyed positional struct literals or post-construction field assignments. Put `//exhaustruct:enforce` on full-mapping, DTO, cloning, and record struct literals so `exhaustruct_v5` catches missed fields when structs change.
- Follow the canonical import aliases in `.golangci.yml` (`docker`, `kit`, `dockerInternal`, `charmLog`, `composeTypes`); otherwise use default import names with no redundant aliases, and alias only to fix a real conflict.
- Use `encoding/json/v2` (not v1), stdlib `uuid` (not `github.com/google/uuid`), `github.com/containerd/errdefs` (not `github.com/docker/docker/errdefs`), and `go.yaml.in/yaml/v4` (not `gopkg.in/yaml.v3`).
- Use `cmp.Or` for defaults instead of `if x == ""`, and `kit.FromPtr` or `new(v)` instead of `p != nil && *p` chains. Do not add nil-to-empty guards for slices or maps (json v2 encodes them as `[]` and `{}`).
- Do not nil-check injected dependencies (tests pass fakes), do not use `Internal` suffixes, and do not write duplicate or forwarding wrappers or one-line helpers.
- Keep Go declarations ordered `type`, `const`, `var`, `func` in single blocks, avoid naked returns and variable shadowing, and use `t.Context()` instead of `context.Background()` or `context.TODO()` in tests.
- Avoid bool flags and long parameter lists on exported Go functions (use an options struct), and use an `fx.In` params struct for constructors with 5+ dependencies.

## Verifying

- Do not write unit tests. A single regression test for a bug fix is fine—at most one, placed at the service layer or its owning logic package (never add handler tests).
- Prefer real verification with proper evidence (actual runtime/API/CLI output, or before/after images and short videos for UI changes) over writing unit tests.
- Update existing tests if your changes affect them. Run the narrowest relevant coverage first, then the matching repo target (`just test backend`, `just test cli`). `just test e2e` and `just test all` require the E2E environment; never start a dev stack just to satisfy a test target.
- After every change, including docs, run `just format all` then `just lint all`. Fix every reported issue and keep the formatter's output.
- Report what ran, what passed, your verification evidence, and what you could not verify. Never claim manual verification that did not happen.

## Pull requests

- Never make a PR unless the developer explicitly asks you to do so.
- Conventional commit titles, plain language: `fix(frontend): shows images correctly in table`.
- Body: the problem in a sentence or two, then how you fixed it. End with the model and harness that did the work.
- UI changes need before/after images. Motion or timing needs a short video.
- Upload PR evidence to GitHub. Never commit PR-only screenshots or assets such as `.github/pr-assets/`.

## Additional tips

- Security is important, but should not be over-indexed on, especially for dev mode/maintainer-only features.
- Before a security audit or triaging a vulnerability report, read [SECURITY.md](SECURITY.md) for the trust model and what is intended.

## Backend

- Domain packages under `backend/internal/<domain>/` use `module.go` (composition and route registration), `service.go` (business operations and orchestration), `model.go` (persistence models), `handler.go` (HTTP input and output), and `helpers.go` (package-local helpers). Create a file only when it has a real responsibility; no placeholders.
- Substantial child features live in `<domain>/children/<feature>/` using the same file set (for example, `image/children/patch` or `system/children/backup`), with no Go files in `children/` itself. Only the immediate parent imports a child (recursively); children never import their parent. Parents coordinate children, keep child instances private, and never expose child types in signatures or inject them outside. Shared persistence models go in the parent's `model.go`; shared contracts go in `types/`.
- Filenames in business domains, their children, and `middleware/` (`cors.go`, `csrf.go`, `environment.go`) are single words with no underscores, except build tags (`service_playwright.go`, `service_buildables.go`, `helpers_unix.go`, `helpers_nonunix.go`). Tests sit beside the production file with the exact basename plus `_test.go` (`service.go` -> `service_test.go`). Never create a production file just to justify a test name. Excluded: `bootstrap`, `common`, `config`, `database`, `di`, `scheduler`, `backend/pkg`, CLI packages, and shared types.
- Echo v5 is the router and Huma v2 handles typed REST/OpenAPI endpoints. Register permissioned endpoints with `middleware.RegisterWithPermission`. Use direct Echo routes only for WebSockets (including diagnostics streams), the agent tunnel connection, the environment proxy, and embedded frontend delivery; everything else goes through Huma, including webhooks, pprof, and Playwright support.
- Handlers translate HTTP data and call services with no business logic. Services get dependencies through constructors or Fx.
- Use context-aware `slog` calls (`InfoContext`, `WarnContext`, `ErrorContext`) with static messages and camelCase key-value pairs, standard `errors` with `fmt.Errorf("…: %w")` for wrapping, `internal/common.Classify` for semantic errors, and `types/base.FieldError` for validation fields.
- Persistence models use `database.BaseModel` and DB helpers, reuse GORM relationships and `Preload`, and stay separate from `types/` API contracts.
- Migrations are Goose Go files in `backend/internal/database/migrations` named `NNNNN_name.go` (version 94 is the Arcane v2.17.1 baseline from `baseline/{sqlite,postgres}.sql`; older databases upgrade to v2.17.1 first). Register them in `init()` with `goose.AddMigrationContext(run(…), run(…))`, using `AddMigrationNoTxContext` only when work cannot run in a transaction (such as SQLite rebuilds with `PRAGMA foreign_keys=OFF`). Keep raw SQL in top-level constants—never import GORM, domain packages, or `types/` (enforced by lint)—and pass the same constant twice to `run(sqlite, postgres)` when dialects match. Provide a real `down` unless it is an irreversible data cleanup, and add an up/down test in `backend/internal/database/database_test.go`.
- Background jobs implement the scheduler job contract, are wired in `internal/di`, and are registered in `internal/bootstrap/jobs_bootstrap.go`. Keep single-operation jobs ordinary. For multi-step, fan-out, or resumable jobs, use `backend/internal/scheduler/flow` (Francis workflows in coordinator runs): follow the package doc migration recipe, make steps idempotent service methods, return `scheduler.Outcome` from the last step, register a `flow.Job`, and guard version bumps with `flowtest.AssertDefinitions`.

## Frontend

- Use Svelte 5 runes (`$props`, `$state`, `$derived`, `$effect`). Never use `export let`, `$:`, `on:event`, `$$props`, `$$restProps`, or legacy slots.
- API clients extend `BaseAPIService`. Reuse existing services, shared components, and query/mutation patterns before making page-local variants. Use precise TypeScript; no `any`.
- Do not stop at `console.error`. Use `handleApiResultWithCallbacks` for one-shot actions, `toast.error(headline, { description: extractApiErrorMessage(err) })` when `Result` does not fit (such as TanStack `onError`), an inline unavailable state for streams and polling, and `throwPageLoadError` in page loads. `APIError.message` already holds the server message—show it, not a canned string.
- Every rendered string goes through Paraglide. Reuse a matching key in `frontend/messages/en.json` first. Append new keys at the end of `en.json` only (Crowdin handles other locales), using `snake_case` from the English text with no domain prefix (`"apply": "Apply"`, `"view_details": "View Details"`, or a short descriptive key for full sentences like `"engine_info_description"`). Existing keys keep their names until touched.

## Environments, permissions, and runtime modes

- Local Docker uses environment ID `"0"` and paths under `/agents/{id}/...`. Await `environmentStore.ready` or `getCurrentEnvironmentId()` before making requests, and redirect environment-specific detail pages when the environment changes.
- Backend permission middleware is the authority; frontend gates are UX only. Keep the permission catalog, access-surface registry, and frontend navigation gates separate. Check global admin via `PermissionSet.IsGlobalAdmin()` or the user DTO's `isGlobalAdmin`; never infer it from a role ID.
- Manager mode serves the UI and manages environments. Edge agent mode runs with `ARCANE_EDGE_AGENT=true` and `ARCANE_MANAGER_API_URL`, dialing the manager.

## Taste

- Put new features in their owning domain, keep one job per file (no god files in services, components, or CLI commands), split by responsibility rather than line count, and avoid trivial packages, needless abstractions, or unrelated restructuring.
- Keep comments short. If the structure needs a paragraph, simplify the code.
- No nested or chained ternaries; use `if`/`else` or `switch`.
- Always use keyed struct and composite literals: one keyed literal per value, never unkeyed positional struct literals or post-construction field assignments. Put `//exhaustruct:enforce` on full-mapping, DTO, cloning, and record struct literals so `exhaustruct_v5` catches missed fields when structs change.
- Follow the canonical import aliases in `.golangci.yml` (`docker`, `kit`, `dockerInternal`, `charmLog`, `composeTypes`); otherwise use default import names with no redundant aliases, and alias only to fix a real conflict.
- Use `encoding/json/v2` (not v1), stdlib `uuid` (not `github.com/google/uuid`), `github.com/containerd/errdefs` (not `github.com/docker/docker/errdefs`), and `go.yaml.in/yaml/v4` (not `gopkg.in/yaml.v3`).
- Use `cmp.Or` for defaults instead of `if x == ""`, and `kit.FromPtr` or `new(v)` instead of `p != nil && *p` chains. Do not add nil-to-empty guards for slices or maps (json v2 encodes them as `[]` and `{}`).
- Do not nil-check injected dependencies (tests pass fakes), do not use `Internal` suffixes, and do not write duplicate or forwarding wrappers or one-line helpers.
- Keep Go declarations ordered `type`, `const`, `var`, `func` in single blocks, avoid naked returns and variable shadowing, and use `t.Context()` instead of `context.Background()` or `context.TODO()` in tests.
- Avoid bool flags and long parameter lists on exported Go functions (use an options struct), and use an `fx.In` params struct for constructors with 5+ dependencies.

## Verifying

- Do not write unit tests. A single regression test for a bug fix is fine—at most one, placed at the service layer or its owning logic package (never add handler tests).
- Prefer real verification with proper evidence (actual runtime/API/CLI output, or before/after images and short videos for UI changes) over writing unit tests.
- Update existing tests if your changes affect them. Run the narrowest relevant coverage first, then the matching repo target (`just test backend`, `just test cli`). `just test e2e` and `just test all` require the E2E environment; never start a dev stack just to satisfy a test target.
- After every change, including docs, run `just format all` then `just lint all`. Fix every reported issue and keep the formatter's output.
- Report what ran, what passed, your verification evidence, and what you could not verify. Never claim manual verification that did not happen.

## Pull requests

- Never make a PR unless the developer explicitly asks you to do so.
- Conventional commit titles, plain language: `fix(frontend): shows images correctly in table`.
- Body: the problem in a sentence or two, then how you fixed it. End with the model and harness that did the work.
- UI changes need before/after images. Motion or timing needs a short video.
- Upload PR evidence to GitHub. Never commit PR-only screenshots or assets such as `.github/pr-assets/`.

## Additional tips

- Security is important, but should not be over-indexed on, especially for dev mode/maintainer-only features.
- Before a security audit or triaging a vulnerability report, read [SECURITY.md](SECURITY.md) for the trust model and what is intended.
