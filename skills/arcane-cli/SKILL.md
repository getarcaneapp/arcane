---
name: arcane-cli
description: |
  Arcane Docker management platform CLI (arcane-cli).
  Use for all operations against the Arcane server: containers, projects
  (Docker Compose), images, volumes, networks, environments, registries,
  GitOps syncs, background jobs, admin, and system tasks.

  Triggers: arcane, arcane-cli, docker management, project up/down,
  container start/stop/redeploy, image updates, GitOps sync, environments.
---

# Arcane CLI Skill

## Critical Facts

- **Binary name**: `arcane-cli`
- **Config file**: `~/.config/arcanecli.yml`
- **Local environment ID**: `"0"` — the Arcane manager's own Docker socket. Always pass `--env <id>` explicitly; if `environments list` returns empty, the API key is missing `environments:list` permission.
- **API key format**: `arc_<hex>` — set via `arcane-cli config set api-key arc_...` or directly in the YAML
- **Output**: `--output json` (or `--json`) for machine-readable output; default is human text
- **Host-specific config** (env IDs, server URL): read `~/.agentSecrets/arcane-cli/secrets.md` before running any commands

## JSON Response Shape

Most list/get commands return `{"success": true, "data": [...], "pagination": {...}}` — index into `.data`.

**Flat responses (no wrapper)** — these commands return a plain object directly:

| Command | Shape |
|---|---|
| `containers counts` | `{runningContainers, stoppedContainers, totalContainers}` |
| `images updates summary` | `{totalImages, imagesWithUpdates, digestUpdates, errorsCount}` |
| `volumes counts` | `{inuse, total, unused}` |
| `networks counts` | `{inuse, unused, total}` |
| `jobs get` | flat object with cron schedule strings per job |
| `updater status` | `{updatingContainers, updatingProjects, containerIds, projectIds}` |
| `settings list` | returns a JSON array directly |
| `system upgrade-check` | `{canUpgrade, error, message}` |
| `system docker-info` | raw Docker daemon info struct |

```bash
# list/get — index .data
arcane-cli containers list --env 0 --json | jq '.data[] | {id, name: .names[0]}'

# flat — use directly
arcane-cli containers counts --env 0 --json | jq '.runningContainers'
arcane-cli images updates summary --env 0 --json | jq '.imagesWithUpdates'
```

Container fields: `id`, `names` (array, use `names[0]`), `image`, `state`, `status`, `ports`, `labels`, `mounts`, `composeInfo`

Project fields: `id`, `name`, `dirName`, `status`, `serviceCount`, `runningCount`, `updateInfo`, `runtimeServices`, `createdAt`, `updatedAt`

## Setup / Auth

```bash
# Show current config
arcane-cli config show

# Set server and key
arcane-cli config set server-url https://your-arcane-server.com
arcane-cli config set api-key arc_xxxxx

# Test connection
arcane-cli config test

# OIDC login (requires OIDC enabled on server)
arcane-cli auth login

# Who am I
arcane-cli auth me

# Switch default environment interactively
arcane-cli environments switch
```

## Global Flags (work on every command)

| Flag | Description |
|---|---|
| `--env <id>` | Override default environment for this call (`"0"` = local) |
| `--output text\|json` | Output mode (alias: `--json`) |
| `--yes` / `-y` | Skip confirmation prompts |
| `--no-color` | Disable ANSI color |
| `--request-timeout <dur>` | e.g. `--request-timeout 2m` |
| `--log-level debug` | Verbose debug output |

## Containers

```bash
arcane-cli containers list                          # running containers
arcane-cli containers list --all                    # include stopped
arcane-cli containers list --limit 50 --start 0    # paginate
arcane-cli containers list --updates has_update     # filter by update status
arcane-cli containers get <id|name>
arcane-cli containers start <id|name>
arcane-cli containers stop <id|name>
arcane-cli containers restart <id|name>
arcane-cli containers redeploy <id|name>           # pull image + recreate
arcane-cli containers update <id|name>             # update container config
arcane-cli containers delete <id|name>
arcane-cli containers counts                        # status counts
arcane-cli containers updates                       # list containers with available updates

# Create a container
arcane-cli containers create \
  --name myapp \
  --image nginx:latest \
  --port 8080:80 \
  --env KEY=VALUE \
  --volume /host:/container \
  --network shared-web \
  --restart unless-stopped

# Create from JSON file
arcane-cli containers create --file container.json
```

## Projects (Docker Compose)

```bash
arcane-cli projects list
arcane-cli projects list --updates has_update      # filter by update status
arcane-cli projects get <id|name>
arcane-cli projects up <id|name>                   # start services
arcane-cli projects down <id|name>                 # stop services
arcane-cli projects restart <id|name>
arcane-cli projects redeploy <id|name>             # pull images + restart
arcane-cli projects pull <id|name>                 # pull latest images only
arcane-cli projects destroy <id|name>              # remove all containers
arcane-cli projects destroy <id|name> --force      # skip confirmation
arcane-cli projects counts

# Create project from compose file
arcane-cli projects create \
  --name myproject \
  --file docker-compose.yml \
  --env-file .env

# Update project (only pass flags you want to change)
arcane-cli projects update <id|name> --file docker-compose.yml
arcane-cli projects update <id|name> --name new-name
arcane-cli projects update-includes <id|name> --file override.yml
```

## Images

```bash
arcane-cli images list
arcane-cli images get <id|name>
arcane-cli images remove <id|name>
arcane-cli images pull nginx:latest
arcane-cli images prune                            # remove unused images
arcane-cli images counts
arcane-cli images upload image.tar                 # upload from tar archive

# Image update checks
arcane-cli images updates check                    # check default image for updates
arcane-cli images updates check-all                # check all images
arcane-cli images updates check-image <image-id>   # check specific image
arcane-cli images updates summary
```

## Volumes

```bash
arcane-cli volumes list
arcane-cli volumes get <volume-name>
arcane-cli volumes delete <volume-name>
arcane-cli volumes prune                           # remove unused volumes
arcane-cli volumes counts
arcane-cli volumes sizes                           # disk usage summary
arcane-cli volumes usage <volume-name>             # specific volume usage
arcane-cli volumes create --name myvolume
```

## Networks

```bash
arcane-cli networks list
arcane-cli networks get <id|name>
arcane-cli networks delete <id|name>
arcane-cli networks prune
arcane-cli networks counts
```

## Environments

Environments are Docker hosts managed by Arcane. ID `"0"` is always the Arcane manager's own local Docker socket. Remote environments have UUID IDs.

**If `environments list` returns empty**, the API key is missing `environments:list` permission — create a new key with full admin permissions. Host-specific env IDs live in `~/.agentSecrets/arcane-cli/secrets.md`.

```bash
arcane-cli environments list
arcane-cli environments get <id>
arcane-cli environments test <id>                  # test connectivity
arcane-cli environments switch                     # interactive default-env picker
arcane-cli environments update <id>
arcane-cli environments version <id>               # get agent version on that env
arcane-cli environments delete <id>
```

## Registries

```bash
arcane-cli registries list
arcane-cli registries get <id>
arcane-cli registries test <id>
arcane-cli registries sync                         # sync registry configs
arcane-cli registries update <id>
arcane-cli registries delete <id>
```

## GitOps

```bash
arcane-cli gitops list
arcane-cli gitops create
arcane-cli gitops get <id|name>
arcane-cli gitops update <id|name>
arcane-cli gitops delete <id|name>
arcane-cli gitops status <id|name>
arcane-cli gitops sync <id|name>                   # trigger a sync now
arcane-cli gitops files <id|name>                  # list files in the git repo
arcane-cli gitops import                           # import sync config
```

## Git Repositories

```bash
arcane-cli repos list
arcane-cli repos create
arcane-cli repos get <repository>
arcane-cli repos update <repository>
arcane-cli repos delete <repository>
arcane-cli repos test <repository>
arcane-cli repos branches <repository>
arcane-cli repos files <repository>
arcane-cli repos sync                              # sync all repos
```

## Background Jobs

```bash
arcane-cli jobs get                                # view job schedules
arcane-cli jobs update                             # update schedule intervals
```

## Updater (auto-update workflow)

```bash
arcane-cli updater status
arcane-cli updater run                             # trigger update run
arcane-cli updater history
```

## Settings

```bash
arcane-cli settings list                           # list environment settings
arcane-cli settings update                         # update environment settings
arcane-cli settings public                         # list public settings
```

## System

```bash
arcane-cli system prune                            # prune all unused resources
arcane-cli system docker-info                      # Docker daemon info
arcane-cli system containers-start-all            # start all containers
arcane-cli system containers-stop-all             # stop all containers
arcane-cli system start-stopped                   # start only stopped containers
arcane-cli system convert "docker run ..."        # convert docker run → compose YAML
arcane-cli system upgrade                          # trigger Arcane self-upgrade
arcane-cli system upgrade-check                   # check if upgrade is available
```

## Admin

```bash
# API keys
arcane-cli admin api-keys list
arcane-cli admin api-keys create <name>
arcane-cli admin api-keys get <id>
arcane-cli admin api-keys update <id>
arcane-cli admin api-keys delete <id>

# Users
arcane-cli admin users list
arcane-cli admin users create
arcane-cli admin users get <user-id>
arcane-cli admin users update <user-id>            # update profile (roles managed separately)
arcane-cli admin users delete <user-id>

# Roles & RBAC
arcane-cli admin roles list
arcane-cli admin roles get <role-id>
arcane-cli admin roles create
arcane-cli admin roles update <role-id>
arcane-cli admin roles delete <role-id>
arcane-cli admin roles permissions                 # full permission manifest
arcane-cli admin roles assignments <user-id>       # list user's current roles
arcane-cli admin roles assign <user-id> \
  --role role_editor:env_prod \
  --role role_viewer                          # replace user's role assignments

# OIDC group → role mappings
arcane-cli admin oidc-mappings list
arcane-cli admin oidc-mappings create --claim docker-admins --role role_admin
arcane-cli admin oidc-mappings update <id>
arcane-cli admin oidc-mappings delete <id>
```

## Templates (Docker Compose templates)

```bash
arcane-cli templates list                          # list local templates
arcane-cli templates default                       # get built-in default templates
arcane-cli templates get <template-id|name>        # get template content
arcane-cli templates content <template-id>         # get raw template content
arcane-cli templates variables <template-id>       # list template variables
arcane-cli templates registries                    # list template registries
arcane-cli templates delete <template-id>
arcane-cli templates delete-registry <registry-id>
```

## Utilities

```bash
arcane-cli generate                                # generate secrets / tokens
arcane-cli auth federated                          # exchange CI OIDC token for Arcane token
arcane-cli doctor                                  # run local CLI diagnostics
arcane-cli version
arcane-cli self-update                             # update the CLI binary
arcane-cli completion bash|zsh|fish|powershell
```

## Config File Reference (`~/.config/arcanecli.yml`)

```yaml
server_url: https://your-arcane-server.com/
api_key: arc_xxxxx
default_environment: "0"
log_level: info
pagination:
  default:
    limit: 25
  resources:
    containers:
      limit: 50
    images:
      limit: 100
    volumes:
      limit: 40
    networks:
      limit: 40
```

Config keys for `arcane-cli config set`: `server-url`, `api-key`, `default-environment`, `log-level`

## Common Patterns

```bash
# JSON output for scripting
arcane-cli containers list --json | jq '.[].name'

# Target a specific environment
arcane-cli containers list --env 2

# Skip confirmation on destructive ops
arcane-cli projects destroy myproject --yes

# Check what's outdated
arcane-cli containers updates --json
arcane-cli projects list --updates has_update

# Redeploy a project (pull + restart)
arcane-cli projects redeploy myproject

# Full system cleanup
arcane-cli system prune --yes
```

## Access Rules

**NEVER SSH into any server without explicit user permission.** Use arcane-cli for all remote container/project operations. SSH to servers is not a substitute for arcane-cli commands.

## Project Env File Rules

Arcane projects use env files (`--env-file`) that hold compose secrets (DB passwords, API keys, tokens). These rules apply whenever you're working with a project's env file — whether reading it locally, passing it to `projects create/update`, or inspecting it on the server.

| Operation | Allowed | How |
|---|---|---|
| View keys | ✅ | `grep -o '^[^#=][^=]*' .env` — keys only, no values |
| Check if a key exists | ✅ | `grep -q '^KEY=' .env && echo set \|\| echo missing` |
| Count entries | ✅ | `grep -c '=' .env` |
| Add a variable | ✅ | `echo 'KEY=value' >> .env` |
| Pass to arcane-cli | ✅ | `arcane-cli projects create --env-file .env` — CLI sends it, Claude never reads it |
| View values | ❌ | Never — not even partially or redacted |
| Output file contents | ❌ | Never `cat`, `Read`, or print the file |

```bash
# ✅ What keys are configured?
grep -o '^[^#=][^=]*' .env

# ✅ How many entries?
grep -c '=' .env

# ✅ Is DATABASE_URL set?
grep -q '^DATABASE_URL=' .env && echo "set" || echo "missing"

# ✅ Deploy with env file — Claude passes the path, never reads the values
arcane-cli projects create --name myapp --file docker-compose.yml --env-file .env

# ❌ Never
cat .env
```

## Pitfalls

- `--env` is the **environment ID** (Docker host), not an env-var flag. On `containers create` specifically, `--env`/`-e` also means environment variable (KEY=VALUE) — context matters.
- Environment ID `"0"` is always the Arcane manager's local Docker socket. It works fine as long as the API key has sufficient permissions.
- If `environments list` returns empty or `success: false`, the API key lacks `environments:list` — generate a new full-admin key.
- `arcane-cli projects destroy` removes all containers — use `arcane-cli projects down` to just stop them.
- `arcane-cli config set` takes key-value pairs: `arcane-cli config set server-url https://... api-key arc_...` (multiple pairs in one call).
- IDs and names are interchangeable for most resources (the CLI resolves either).
