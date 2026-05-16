# Arcane Context

Arcane is a Docker management product with a Go backend, a SvelteKit frontend, shared TypeScript/Go-facing types, and an optional headless agent.

## Glossary

### Arcane Manager

The main Arcane application. It serves the web UI and Huma API, stores application state, manages the local Docker environment, and coordinates remote environments.

### Docker Environment

A Docker host managed by Arcane. The local Docker environment uses ID `0`. Remote environments are managed through standard agents or edge agents.

### Local Docker Environment

The Docker environment available to the Arcane Manager through the local Docker socket. Code and docs should use ID `0` when referring to the local environment.

### Remote Environment

A Docker environment that is not the manager's local Docker socket. Remote environments communicate through an agent API, edge tunnel, or polling transport.

### Agent

A headless Arcane process running near a Docker host. It accepts manager requests and executes them against its local Docker daemon.

### Edge Agent

An agent that connects outbound to the Arcane Manager over the edge transport so Docker hosts behind NAT or firewalls can be managed.

### Direct Agent

An agent that runs as a passive HTTP server on TCP 3553. The manager initiates all connections; the agent never dials out. Suited to one-way network paths (e.g. SSH forward tunnels, restricted-outbound hosts).

### Project

An Arcane-managed Docker Compose project stored under the configured projects directory. Projects have compose files, optional env files, persisted metadata, deployment state, and related Docker resources.

### Stack

A Docker Swarm stack managed through Arcane. Stack source, rendering, deployment, services, and tasks are handled separately from Compose projects.

### Container Registry

A registry configuration Arcane uses for image pulls, image update checks, and remote environment synchronization. Registry handling should remain provider-generic unless a provider-specific path is required.

### Image Update

Arcane's record of whether a container or image reference has a newer digest available. Image update behavior is used by manual checks, scheduled polling, notifications, and updater workflows.

### Vulnerability Scan

Arcane's Trivy-backed scan lifecycle for Docker images. The lifecycle includes scan phase tracking, scanner container setup, result persistence, ignore filtering, summaries, and notifications.

### Job Schedule

The persisted schedule and runtime registration for background jobs such as image polling, auto-update, vulnerability scanning, pruning, GitOps sync, Docker client refresh, and environment health checks.

### GitOps Sync

The workflow that pulls configured Git repositories and applies their project or stack definitions into Arcane-managed resources.

### Notification

An outbound message Arcane sends for update, vulnerability, prune, auto-heal, and test events through configured providers.

## Architecture Notes

- Backend handlers should be thin wrappers over domain behavior.
- Frontend API calls for Docker resources should include the active Docker environment ID.
- Svelte code must use Svelte 5 runes syntax.
- Database models should embed `models.BaseModel` unless a record has a deliberate persistence reason not to.
- Architecture reviews should use this vocabulary when proposing module names or seams.
