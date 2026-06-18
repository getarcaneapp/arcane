Plan: Enforce per-environment permissions on the remote-environment proxy
TL;DR — Fix an authorization bypass where any authenticated user can perform write actions on remote/edge environments regardless of their role. The manager's EnvironmentMiddleware proxies /api/environments/{id}/... to agents after checking only authentication; the agent runs with SudoPermissionSet(), so no permission is ever enforced. Fix: enforce the caller's per-environment permission in the proxy before forwarding, using each operation's required permission as a single source of truth (attached to huma.Operation.Metadata). Local env ("0") is already correctly enforced and stays unchanged.

Phase 1 — Make each operation's permission introspectable (single source of truth)
Add a shared registration helper in role.go (or a sibling file), e.g. RegisterEnvScoped(api, op, perm, handler), that stores perm in op.Metadata[MetaRequiredPermission], attaches RequirePermission(api, perm), and calls huma.Register. The required perm argument makes it impossible to register without one.
Migrate the env-scoped (/environments/{id}/...) handler registrations to the helper — containers, images, image_updates, volumes, networks, ports, system, swarm, projects, dashboard, vulnerabilities (mechanical: drop the Middlewares: line, pass perm as an arg). All of these use a single RequirePermission today; none under /environments/{id}/ uses RequireGlobalAdmin.
Expose a shared slice of the 6 proxied WS routes + their permissions from handler.go:243 (project logs, container logs/stats/terminal, swarm service logs, system stats) so both route registration and the matcher use one source.
Phase 2 — Build the authorization matcher
After SetupAPI in api.go:138, build a (method, env-suffix-path-template) → permission matcher by walking api.OpenAPI().Paths reading Metadata[MetaRequiredPermission], plus the WS slice from step 3. Normalize Huma {x} and Echo :x params to one wildcard form; static segments win over wildcards (so /containers/counts ≠ /containers/{containerId}).
Phase 3 — Enforce in the proxy
Give the proxy the caller's PermissionSet: change AuthValidator in environment_middleware.go:52 to return (\*authz.PermissionSet, bool), and update router_bootstrap.go:90 to resolve it via the existing RoleService.ResolvePermissions / ResolveApiKeyPermissions (env/agent-bootstrap tokens → SudoPermissionSet()). Reuse, no duplication.
In EnvironmentMiddleware.Handle, right after auth-validation and before any proxy branch (covers edge-tunnel + HTTP + WS): look up the required permission for (method, suffix); if the caller isn't Sudo and !ps.Allows(perm, remoteEnvID) → 403 (matching the file's existing JSON error shape). No mapping found → default-deny 403.
Wire the matcher through NewEnvProxyMiddlewareWithParam[AndRegistry] and the bootstrap call.
Phase 4 — Tests & verification
Unit tests: matcher static-vs-{param} precedence; remote write denied for read-only role; remote read allowed; agent/env token still allowed; local path untouched; WS terminal denied without containers:exec.
Guard test: every /environments/{id}/... Huma op carries permission metadata (also catches any unprotected env-scoped endpoint).
Run cd backend && go test ./api/... ./internal/services/... ./internal/middleware/... ./internal/bootstrap/... ./pkg/authz/....
Manual: run the issue's repro against a remote env → expect 403; confirm an allowed role works and local is unaffected.
Relevant files

environment_middleware.go:95 — authz check in Handle; AuthValidator signature; constructor takes the matcher.
router_bootstrap.go:90 — resolve PermissionSet; build/pass matcher.
api.go:138 — build matcher from api.OpenAPI() metadata.
role.go:19 — RegisterEnvScoped helper + MetaRequiredPermission key.
handler.go:243 — shared WS route→permission slice.
backend/api/handlers/{containers,images,image_updates,volumes,networks,ports,system,swarm,projects,dashboard,vulnerabilities}.go — migrate env-scoped registrations.
Decisions (confirmed)

Single source of truth via operation metadata + shared helper (Approach A).
Covers all env-scoped resources, not just containers.
Unmapped env-scoped path to a remote env → default-deny (403).
Scope boundaries

Excluded: local env enforcement (already correct), org-level endpoints (not proxied), management paths under /environments/{id}/ already handled locally (isManagementPathInternal), and the frontend (backend enforcement is authoritative; UI gating is out of scope here).
Audit item during implementation: confirm no buildables/playwright/direct-Echo routes sit under /environments/{id}/ that default-deny would wrongly block (initial grep found none).
Want me to proceed with this plan, or adjust anything (e.g., migrate all Huma registrations to the helper for uniformity vs. only the env-scoped ones)?
