---
name: arcane-deploy
description: >-
  Deploy the Arcane Docker management UI and its remote agents with Docker
  Compose: the manager compose file, ARCANE_ENCRYPTION_KEY, projects
  directory mounts, PUID/PGID, the agent compose file with
  ARCANE_AGENT_TOKEN, and the Podman and Docker socket-proxy examples. Use
  when installing Arcane or adding a host for it to manage.
---

# Deploying Arcane

References (on `getarcaneapp/arcane` `main`):
[compose.basic.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.basic.yaml),
[compose.agent.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.agent.yaml),
[compose.proxy.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.proxy.yaml)
(Docker socket proxy, not HTTPS),
[compose.podman-rootful.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.podman-rootful.yaml),
[compose.podman-rootless.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.podman-rootless.yaml),
[quadlet/](https://github.com/getarcaneapp/arcane/tree/main/docker/examples/quadlet),
https://getarcane.app.

## Manager

Start from [compose.basic.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.basic.yaml):

```yaml
services:
  arcane:
    image: getarcaneapp/manager:latest
    ports: ['3552:3552']
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - arcane-data:/app/data
      - /srv/compose:/srv/compose # same path inside and outside
    environment:
      - ARCANE_ENCRYPTION_KEY=<random value, 32+ chars>
      - ARCANE_PROJECTS_DIRECTORY=/srv/compose
      - TZ=UTC
    cgroup: host
    restart: unless-stopped
volumes:
  arcane-data:
```

- `ARCANE_ENCRYPTION_KEY` must be at least 32 characters in production
  (or a `hex:`/`base64:` 32-byte key; `ARCANE_ENCRYPTION_KEY_FILE` also
  works). Keep it stable: it protects stored secrets. Generate one with
  `openssl rand -base64 32` or `arcane-cli generate secret --format env`.
- Mount the projects directory at the same path on host and in the
  container so Compose relative paths resolve.
- `PUID`/`PGID` run Arcane as a host user; the Docker socket group is
  mapped automatically.
- Mounting the Docker socket gives Arcane root-equivalent control of the
  host. Expose port 3552 only behind TLS; see
  [TLS](https://getarcane.app/docs/networking/tls) and
  [reverse proxy](https://getarcane.app/docs/networking/reverse-proxy).
  [compose.proxy.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.proxy.yaml)
  is a Docker socket proxy only — it does not terminate HTTPS.

Health: the image's healthcheck runs `./arcane health`; the API answers
`GET /api/health`.

## Remote hosts (agents)

Create an agent in the manager (or `arcane-cli agents create --name <host>`) to get a token,
then on the remote host run the agent image (from
[compose.agent.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.agent.yaml)):

```yaml
image: getarcaneapp/agent:latest
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
  - agent-data:/app/data
environment:
  - ARCANE_AGENT=true
  - ARCANE_AGENT_TOKEN=arc_...
  - ARCANE_MANAGER_API_URL=https://arcane.example.com
  - ARCANE_AGENT_TRANSPORT=auto # grpc preferred, websocket fallback
```

Check it with `arcane-cli agents list` and `arcane-cli agents test <id>`.

## Podman

Download
[compose.podman-rootful.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.podman-rootful.yaml)
or
[compose.podman-rootless.yaml](https://github.com/getarcaneapp/arcane/blob/main/docker/examples/compose.podman-rootless.yaml),
or use the
[Quadlet units](https://github.com/getarcaneapp/arcane/tree/main/docker/examples/quadlet).
