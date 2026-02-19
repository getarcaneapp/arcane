> Generated: 2026-02-19 | Branch: `feat/docker-swarm` | HEAD: `efec888aaf8c7efd9a55d96db74580d2f6054231`

---

# Docker SDK Swarm Deprecation — Options Analysis

## Context

The upstream maintainer flagged that the Docker Go SDK has deprecated swarm functions, raising questions about the viability of the `feat/docker-swarm` branch. This document analyses the current situation and the options available.

---

## The Actual Problem

The branch uses `github.com/docker/docker v28.5.2+incompatible`.

Docker/Moby has a **two-track SDK situation**:

| SDK | Import path | Swarm support | Status |
|-----|-------------|---------------|--------|
| Legacy SDK | `github.com/docker/docker` | Full | Deprecated path, still functional |
| Moby client | `github.com/moby/moby/client` | Full | New canonical path, actively maintained |
| New go-sdk | `github.com/docker/go-sdk` | **None** | WIP, not yet v1.0 — covers containers/images/networks/volumes only |

The swarm client methods themselves (`ServiceCreate`, `ServiceList`, `ServiceUpdate`, `ServiceRemove`, `NodeList`, `TaskList`, `SwarmInspect`, `ConfigCreate`, `SecretCreate`) are **not removed or deprecated** — they exist in both `github.com/docker/docker` and `github.com/moby/moby/client`.

The maintainer's concern is almost certainly about the **new `github.com/docker/go-sdk`**, which the project may be tracking or planning to migrate to. That SDK has zero swarm support and is explicitly WIP.

---

## SDK Options

### Option 1 — Stay on `github.com/docker/docker` / migrate to `github.com/moby/moby/client`

**What:** Keep using the existing moby client (or rename the import to `moby/moby`). All swarm calls work unchanged.

**Pros:**
- Zero code change to the swarm feature itself
- All 13 swarm client methods used in the branch are fully supported
- `moby/moby/client` is actively maintained with no swarm removal planned

**Cons:**
- If the project later migrates to `go-sdk`, swarm support will hit a wall again
- Keeps a second "heavy" SDK dependency alongside whatever future SDK direction the project takes

**Risk:** Medium-term — depends on how aggressively the project pursues `go-sdk` migration.

---

### Option 2 — Reimplement swarm via direct Docker Engine REST API

**What:** Drop the `client.ServiceList()` / `client.NodeList()` etc. calls. Instead, call the Docker Engine HTTP API directly (the same API the SDK wraps) using plain HTTP calls against the Docker socket.

**Pros:**
- Completely SDK-independent — immune to any future SDK deprecation
- The Docker Engine REST API for swarm is stable and documented
- The project already uses the Docker socket for other operations — the transport layer exists
- Type definitions from `github.com/docker/docker/api/types/swarm` can still be reused (or inlined) for unmarshalling

**Cons:**
- Significantly more boilerplate — each call needs manual URL construction, request serialisation, response deserialisation, and error handling
- Filters, versions, and query params must be constructed manually
- ~500–700 lines of new HTTP client code to replace the service layer

**Risk:** Low long-term. High upfront effort.

---

### Option 3 — Abstract behind an interface now, swap implementation later

**What:** Define a `SwarmClient` interface in the service layer. Provide a moby-client-backed implementation today. When/if `go-sdk` gains swarm support, swap the implementation without touching the handlers or types.

**Pros:**
- Addresses the maintainer's concern architecturally without blocking the feature
- Minimal immediate work over Option 1
- Clean seam for future migration

**Cons:**
- Still depends on `github.com/docker/docker` under the hood for now
- Interface design requires care to avoid leaking SDK types

**Risk:** Low — buys time while the SDK situation settles.

---

### Option 4 — Hold the feature until `go-sdk` gains swarm support

**What:** Don't merge the branch. Wait for `github.com/docker/go-sdk` to add swarm APIs.

**Pros:** Clean — no technical debt.

**Cons:**
- `go-sdk` has no swarm support on its roadmap currently; timeline is unknown
- The feature is already fully implemented and functional

**Risk:** Could mean indefinite delay.

---

### Recommendation

**Option 3** is the most pragmatic path forward:

1. Rename the import to `github.com/moby/moby/client` (the new canonical path) — this resolves the `+incompatible` / deprecated-path concern with minimal effort
2. Wrap the swarm Docker client calls behind a `SwarmDockerClient` interface in the service layer
3. Ship the feature; the interface gives a clean migration point if/when `go-sdk` ever adds swarm support

If the maintainer's actual concern is "we don't want this dependency at all", then **Option 2** (direct REST) is the only path that fully satisfies that.

---

# Swarm Feature: Working / Broken / Missing Assessment

## Working

### Backend
| Area | Operations |
|------|-----------|
| Services | List (paginated/searchable), Inspect, Create, Update, Delete |
| Nodes | List (paginated), Inspect |
| Tasks | List (paginated, with service+node name enrichment) |
| Stacks | List (inferred from `com.docker.stack.namespace` labels), Deploy from Compose YAML |
| Swarm info | Cluster metadata |
| Stack deploy engine | Networks (overlay, external, IPAM), Secrets (file/env/inline), Configs (file/env/inline), Volumes (bind/volume/tmpfs/image), Deploy config (replicas, update/rollback policy, restart policy, placement), Ports, Container config (image, command, env, mounts, labels, user, hostname, etc.), DNS, Extra hosts, Network attachment |
| Pagination | Fully functional across all list operations with search + sort |
| Error handling | Swarm-not-enabled (409), non-manager (403), not-found (404), wrapped errors |

### Frontend
| Area | Operations |
|------|-----------|
| Services | List, Create (full form), Edit (JSON spec + form fields), Delete |
| Nodes | List (read-only) |
| Tasks | List with colour-coded state badges (read-only) |
| Stacks | List, Deploy (Monaco YAML editor, env file, templates, docker-run converter) |
| Navigation | Swarm section in sidebar + mobile nav |
| UX | Pagination, sorting, mobile-responsive field toggles, toast error handling throughout |

---

## Broken (implemented but has known defects)

| Issue | Severity | Location |
|-------|----------|----------|
| `WithRegistryAuth`: `QueryRegistry` flag set but `EncodedRegistryAuth` is never populated → private registry auth doesn't actually work | High | `stack_deploy_engine.go:409` (TODO comment present) |
| `ResolveImage` option accepted by API and wired through all layers, then **silently ignored** in the deploy engine | High | `stack_deploy_engine.go` — `opts.ResolveImage` never read |
| Missing secret/config silently skipped with `continue` instead of returning error → phantom deployment failures | High | `stack_deploy_engine.go:621-623` |
| Health checks in Compose spec **completely ignored** — services deploy without health check config | High | `stack_deploy_engine.go` |
| Logging driver/options in Compose spec **completely ignored** | High | `stack_deploy_engine.go` |
| Network name fallback: if network not found in map, silently falls back to key name which may be wrong | Medium | `stack_deploy_engine.go:557-559` |
| Port parsing failure silently drops port entry with no warning | Medium | `stack_deploy_engine.go:535-538` |
| Prune only removes services — orphaned networks, secrets, and configs accumulate | Medium | `stack_deploy_engine.go:101-110` |
| `console.warn()` left in stack create page loader (3 instances) | Low | `stacks/new/+page.ts:8,12,17` |
| `console.error()` left in service editor dialog | Low | `service-editor-dialog.svelte:112` |
| `Privileged`, `CapAdd`, `CapDrop`, `SecurityOpt` from Compose all ignored | Medium | `stack_deploy_engine.go` |

---

## Missing (not implemented)

### Backend — API endpoints not present
| Feature | Notes |
|---------|-------|
| **Stack delete** | Can deploy stacks but cannot remove them |
| **Stack inspect** | No per-stack detail endpoint |
| **Node update** (drain / promote / demote / set availability) | Cluster operations impossible |
| **Node remove** | Can't evict a node via the API |
| **Task inspect** | Only list available |
| **Service logs / Task logs** | No streaming (would need WebSocket like container logs) |
| **Secrets CRUD** | Secrets are consumed by deploy engine but can't be managed independently |
| **Configs CRUD** | Same as secrets |
| **Swarm init / join / leave** | Cluster lifecycle not supported (intentional or not?) |

### Frontend — UI gaps
| Feature | Notes |
|---------|-------|
| **Stack delete action** | Stacks table has no row actions at all |
| **Stack inspect / detail page** | No drill-down |
| **Node actions** (drain, promote, demote) | Read-only table, no actions |
| **Node detail page** | No drill-down |
| **Task detail page** | No drill-down |
| **Task / service logs** | No log viewer |
| **Service detail page** | Only inline edit dialog, no dedicated page |
| **Service quick-scale** | No replica slider — must edit full spec |
| **Swarm mode gating** | Nav items may always show regardless of whether the connected host is actually in swarm mode — needs verification |
| **Search on nodes/tasks/stacks** | No search input on these pages |

---

## Overall Completeness Estimate

| Layer | Completeness |
|-------|-------------|
| Backend service CRUD | ~90% |
| Backend node/task/stack mgmt | ~40% |
| Stack deploy engine | ~65% (core works; auth/health/logging/prune gaps) |
| Frontend read views | ~80% |
| Frontend write/action capabilities | ~45% |
| **Overall** | **~60%** |

The feature is solid as a read-only monitoring dashboard and covers the most common service management workflow. The main gaps are: stack deletion, node management actions, private registry auth, and health/logging config in the deploy engine.
