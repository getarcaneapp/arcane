# Design: Agnostic External Secrets Providers

Status: **Draft / RFC**
Owner: @manawenuz
Last updated: 2026-06-14

## Summary

Add pluggable **external secrets providers** to Arcane so a Docker Compose project's
environment can be resolved at deploy time from a secrets manager (Bitwarden Secrets
Manager / `bws`, Infisical, and later Vault/OpenBao/SOPS) instead of from plaintext on
disk. Provider credentials are encrypted at rest; resolved secret **values are held in
memory only and never written to disk**.

The abstraction is provider-neutral; the first two implementations are `bitwarden`
(speaking the BWS wire protocol natively in Go — works against either a self-hosted
sidecar or Bitwarden cloud) and `infisical`.

## Goals

- One Go interface, many backends, selected by config.
- Resolve `${{ secrets.<provider>.<KEY> }}` references in a project's env at deploy time.
- Credentials encrypted at rest via the existing `pkg/libarcane/crypto` (AES-GCM).
- Resolved values **never** persisted to disk; injected in-memory into the Compose
  `Environment` map only.
- Resilient: per-provider last-known-good cache so a provider/network outage doesn't
  block a redeploy.

## Non-goals (v1)

- Writing secrets back to a provider from Arcane.
- Rotation, dynamic/leased secrets, PKI.
- Per-secret RBAC beyond what the provider already enforces on its access token.

Read-only resolution first; the above are follow-ups.

---

## Architecture

### Provider interface

New package `backend/pkg/secrets`; shared DTOs in `types/secretsprovider/`. Mirror the
existing pluggable pattern used by `RegistryDaemonClient`
(`backend/internal/services/container_registry_service.go`).

```go
package secrets

// Provider is a read-only external secrets backend.
type Provider interface {
    // Kind identifies the backend: "bitwarden", "infisical", ...
    Kind() string

    // Resolve fetches the requested keys for a scope. Returned values are plaintext,
    // in-memory only: implementations MUST NOT log or persist them.
    Resolve(ctx context.Context, scope Scope, keys []string) (map[string]string, error)

    // HealthCheck validates credentials/connectivity (used by the "Test connection" UI).
    HealthCheck(ctx context.Context) error
}

// Scope is interpreted per provider (e.g. bws: organization+project; infisical: project+env).
type Scope struct {
    Project     string
    Environment string
    Path        string
}

// Factory builds a Provider from decrypted, kind-specific config.
type Factory func(cfg map[string]string) (Provider, error)
```

Providers register into a config-driven registry (`map[string]Factory`) wired in
`backend/internal/di` (a `provideSecretsProviders` set), the same way registry/daemon
getters are wired today.

### Data model (GORM, embed `BaseModel`, migrations for **both** sqlite + postgres)

- `secret_providers`: `id, name, kind, config_json, enabled`.
  `config_json` holds kind-specific config and is **encrypted at rest** via
  `pkg/libarcane/crypto.Encrypt` (same path registry tokens use). Decrypted only at
  resolve time.
  - bitwarden config: `server_base_url`, `access_token`
  - infisical config: `site_url`, `client_id`, `client_secret`, `project_id`
- `project_secret_bindings`: `project_id, provider_id, scope_path, enabled, fail_mode`
  (`fail_mode` ∈ `fail_closed` | `fail_stale`).

### Reference syntax (declarative, provider-agnostic)

A project declares which env values come from a provider, in the env value itself:

```
DB_PASSWORD=${{ secrets.bitwarden.DEV_DB_URL }}
API_KEY=${{ secrets.infisical.STRIPE_KEY }}
```

`secrets.<providerName>.<KEY>` — `providerName` resolves to a configured provider (scoped
to the project via a binding); `KEY` is the secret name in that provider.

### Injection seam (single integration point)

Resolution hooks into `backend/pkg/projects/env.go` / `load.go::LoadEnvironment`, **after**
the existing merge (process env → `.env.global` → project `.env` → `project.env` override)
and **before** the map is handed to compose-go:

1. Scan the merged env map for `${{ secrets.* }}` references.
2. Group references by provider; for each, call `Resolve(scope, keys)` once (batched).
3. Substitute values into the in-memory map.
4. Hand the map to `docker/compose` as `ConfigDetails.Environment`.

Projects with no references make no provider calls. No other part of the deploy path
changes.

### Resilience

Each provider wraps a **last-known-good cache** (keyed by provider+scope+key, TTL
configurable). On a provider/network failure:

- `fail_stale` (default for non-critical): serve cached values, mark deploy as using
  stale secrets, surface staleness in UI/events.
- `fail_closed`: abort the deploy with a clear error.

---

## Security requirements (hard)

1. **No plaintext on disk.** Resolved values are injected in-memory only and MUST NOT be
   written into `.env` / `EffectiveEnvFileName` or any persisted file. This is a
   deliberate deviation from the current disk-materialization flow.
2. **No plaintext in logs.** Resolver and providers must never log secret values or
   provider tokens. Enforced by a unit test.
3. **Encrypted at rest.** All provider credentials encrypted via the existing AES-GCM
   helper; decrypted only at resolve time, never returned to API clients.
4. **Redaction in API/UI/events.** Only key *names* are ever exposed; values are masked.
5. **Least privilege.** Provider access tokens should be read-only and project-scoped
   where the backend supports it.

---

## API (Huma v2 typed handlers, registered in `api/api.go`; permissions from `pkg/authz`)

- `GET/POST/PATCH/DELETE  …/secret-providers`            — CRUD (values write-only/masked)
- `POST  …/secret-providers/{id}/test`                   — HealthCheck
- `GET/POST/DELETE  …/projects/{id}/secret-bindings`     — bind a provider+scope
- `GET  …/projects/{id}/secret-refs`                     — dry-run: list `${{ secrets.* }}`
  refs found and whether each resolves (names only, never values)

## Frontend (`frontend/src/routes/(app)/settings/secret-providers/` + per-project tab)

Follow the registries/authentication settings pattern: Svelte 5 runes, shadcn-svelte,
`createSettingsForm`, Paraglide i18n, a service extending `BaseAPIService`.

- Settings: list/add/edit providers, "Test connection".
- Per-project "Secrets" tab: bind provider + scope, preview detected refs + resolve status
  (names only).

## CLI (`cli/pkg/secrets/`, Cobra)

- `arcane secrets providers ls|add|test`
- `arcane secrets bind <project>`
- `arcane secrets check <project>` — dry-run resolve (names only)

---

## Providers

### `bitwarden` (BWS wire protocol, native Go)

Implemented as a native Go HTTP client of the BWS read path — **no `bws` binary
dependency and no proprietary SDK** (keeps it mergeable and license-clean). Because it
speaks the wire protocol, the same provider works against **either** a self-hosted sidecar
**or** Bitwarden cloud. **Correction:** the two differ in **both host and path prefix**, not
only `server_base_url` — cloud is split-host with no prefixes (`identity.bitwarden.com/connect/token`,
`api.bitwarden.com/organizations/...`); self-host derives `{base}/identity` and `{base}/api`. The
provider therefore needs two topology modes. See **[Appendix A](#appendix-a--cloud-vs-self-host-topology)**.

Read-path flow:

1. `POST {base}/identity/connect/token`
   `grant_type=client_credentials&scope=api.secrets&client_id=<uuid>&client_secret=<secret>`
   → `{ access_token (bearer), expires_in, token_type, scope, encrypted_payload }`.
2. Derive the token key from the access token's 16-byte seed:
   `HKDF-SHA256(ikm=seed, salt="bitwarden-accesstoken", info="sm-access-token", L=64)`
   → `enc[0:32] || mac[32:64]`.
3. Decrypt `encrypted_payload` (type-2 EncString `2.iv|ct|mac`, AES-256-CBC + HMAC-SHA-256
   over `iv‖ct`, PKCS#7) → `{ "encryptionKey": <base64 64-byte org key> }`.
4. `GET {base}/api/organizations/{org}/secrets` (org from JWT `organization` claim), then
   `GET {base}/api/secrets/{id}` (or `POST {base}/api/secrets/get-by-ids`) → decrypt the
   type-2 EncString `key`/`value` fields with the org key.

Access token string format: `0.<client_id>.<client_secret>:<base64(seed16)>`.

### `infisical`

Universal Auth (Client ID/Secret) → REST `GET /api/v3/secrets` for `project_id` +
environment. Standard bearer flow; no bespoke crypto.

---

## Requirements ON the bws-sidecar server (for the `bitwarden` provider)

These are the server-side contracts the Go provider depends on. (The sidecar's read-path
MVP already satisfies most.)

1. **Single-host self-hosted topology.** The provider is configured with one
   `server_base_url` and derives `{base}/identity` and `{base}/api` from it. The server
   MUST mount identity routes under `/identity` and the SM API under `/api` off that base.
   *(Confirmed empirically — `bws -u <base>` derives `/identity` + `/api` and the sidecar
   mounts exactly those; matrix #11 to be marked resolved.)* The sidecar can additionally be
   configured for **drop-in cloud compatibility** (serving cloud-style unprefixed paths), so a
   cloud-configured client works against it unchanged — see **[Appendix A](#appendix-a--cloud-vs-self-host-topology)**.
2. **Frozen read-path contract** with the exact captured JSON shapes:
   `POST /identity/connect/token`, `GET /api/organizations/{org}/secrets`,
   `GET /api/secrets/{id}`, `POST /api/secrets/get-by-ids`. Treat these as a stable API.
3. **HTTPS** with a configurable/served certificate (or run behind a reverse proxy). The
   provider talks to it over TLS.
4. **Token provisioning path** (`bws-provision` equivalent) so an operator can mint a
   read-only `access_token` to paste into the Arcane provider config.
5. **Cheap health endpoint** (`GET /api/status` or `/alive`) for `HealthCheck()`.

Property worth preserving: the provider being a native protocol client (not a wrapper
around the sidecar) is exactly what makes it agnostic and upstream-mergeable.

---

## PR slicing (to land incrementally)

1. Interface + DB models + migrations + crypto wiring + resolver seam + tests (no provider
   yet; resolver is a no-op until a provider is registered).
2. `bitwarden` provider (+ its own tests, including the crypto chain against a fixture).
3. `infisical` provider.
4. API (Huma handlers + authz).
5. Frontend (settings + per-project tab).
6. CLI (`arcane secrets`).

## Mergeability checklist

Conventional Commits; Huma typed I/O on Echo (no new API standard); GORM model with
**sqlite + postgres** migrations; `go test ./...` with in-memory sqlite and interface
mocks for providers; a test asserting no plaintext secret hits logs; ESLint/Prettier +
Svelte 5 runes; Paraglide i18n strings.

## Open questions

- Reference syntax: `${{ secrets.x.y }}` vs `arcane+secret://x/y` — pick one and document.
- Precedence when a key is both in `.env` and referenced — default: explicit ref wins.
- Cache TTL defaults and whether staleness is per-binding or global.

---

## Appendix A — Cloud vs self-host topology

Added 2026-06-14. Captures a correction and the sidecar capability decided while this PRD was in
flight (the bws-sidecar implementation has already started; this is an appendix to that work).

### Topologies differ in host *and* path prefix
Empirically captured (black-box, our own credentials — see the sidecar's
`docs/03-spec/captures/RESULTS.md`):

| | Identity token endpoint | SM API base |
|---|---|---|
| **Cloud** | `https://identity.bitwarden.com/connect/token` (no `/identity`) | `https://api.bitwarden.com/organizations/...`, `/secrets/...` (no `/api`) |
| **Self-host (sidecar)** | `{base}/identity/connect/token` | `{base}/api/organizations/...`, `{base}/api/secrets/...` |

So "differing only by `server_base_url`" is inaccurate. The `bitwarden` provider config should
therefore support **either**:
- a single `server_base_url` (self-host; derive `{base}/identity` + `{base}/api`), **or**
- explicit `identity_url` + `api_url` (cloud, or any split/advanced deployment).

### What is already 100% cloud-identical
Crypto chain (HKDF `salt="bitwarden-accesstoken"`, `info="sm-access-token"`, 64-byte split;
type-2 EncString `2.iv|ct|mac`), the JWT claim shape (`type:ServiceAccount`, `sub≠client_id`,
`organization`, `scope:["api.secrets"]`), the `encrypted_payload` wrap, and the request/response
JSON for the read+write endpoints. A client pointed at the sidecar behaves as against cloud through
all of these.

### Making the sidecar drop-in cloud-compatible (configurable)
Two pieces close the remaining gap; both are sidecar-side and optional per deployment:

1. **Path topology (the real divergence).** Add `BWS_SIDECAR_COMPAT = both | selfhost | cloud`
   (default `both`): serve the routes at the prefixed (`/identity`, `/api`) *and* the cloud-style
   unprefixed paths. With `both`, a client configured for either topology works unchanged. (~small
   change; planned.)
2. **Token signing fidelity (optional).** Cloud signs the bearer **RS256** and exposes OIDC
   discovery + JWKS; the sidecar currently uses **HS256**. This does **not** affect `bws` or this
   provider — both relay the bearer opaquely and never validate its signature. RS256 + a
   `/.well-known/openid-configuration` + JWKS endpoint is only needed for a strict
   signature-validating client; treat as a follow-up.

### Boundary (what "100%" can and cannot mean)
- Achievable: **wire-compatibility** so any BWS client *pointed at the sidecar's URL* behaves
  identically to cloud. Not achievable: *being* `identity.bitwarden.com`/`api.bitwarden.com` (DNS +
  cert chain — the client must be configured to reach the sidecar, or override DNS and trust its cert).
- Scope: "100%" = the **Secrets Manager / `bws` surface** captured here (identity + projects/secrets
  read+write), not the entire Bitwarden cloud API (web vault, service-account admin CRUD, billing,
  events are out of scope).
