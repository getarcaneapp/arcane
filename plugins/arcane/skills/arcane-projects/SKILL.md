---
name: arcane-projects
description: >-
  Manage Docker Compose projects in Arcane from the CLI: create from a
  compose file, list and filter, up/down/restart/redeploy, check and apply
  image updates, build, run jobs, read workspace files, export/import with
  volume data, copy or move between agents, destroy, and GitOps syncs. Use
  when deploying or maintaining Compose stacks through Arcane.
---

# Arcane projects (Compose stacks)

Reference: [`cli/pkg/projects`](https://github.com/getarcaneapp/arcane/tree/main/cli/pkg/projects) and [`cli/pkg/gitops`](https://github.com/getarcaneapp/arcane/tree/main/cli/pkg/gitops).

Commands that create something on a host (`projects create`, `projects
import`) need `--agent <id>` when run non-interactively or with `--json`,
even for the manager's own Docker host. Get the id from
`arcane-cli agents list`.

## Create and inspect

```bash
arcane-cli projects create --agent <id> --name web --file compose.yaml --env-file .env
arcane-cli projects list --status running --updates has_update
arcane-cli projects get web
arcane-cli projects workspace cat web compose.yaml
```

## Lifecycle

```bash
arcane-cli projects up web
arcane-cli projects down web
arcane-cli projects restart web
arcane-cli projects pull web
arcane-cli projects redeploy web             # pull images and restart
arcane-cli projects build web --service api --push
arcane-cli projects run-job web migrate      # streams output
```

## Updates

```bash
arcane-cli projects updates                  # projects with newer images
arcane-cli projects upgrade web              # all services with a newer image
arcane-cli projects upgrade web api worker   # only these services
```

## Backup, migrate, remove

```bash
arcane-cli projects export web web.tar.gz --all-volumes --stop   # include volume data
arcane-cli projects import web.tar.gz --agent <id>
arcane-cli projects copy web --to <agent-id> --volumes            # stops source while copying volumes
arcane-cli projects move web --to <agent-id> --volumes            # stops source while copying volumes
arcane-cli projects archive web                                   # stopped projects only
arcane-cli projects destroy web --remove-volumes                  # deletes data
```

`destroy` removes containers and, by default, the project's files
(`--remove-files`); `--remove-volumes` also deletes data. `--volumes` on
copy or move stops a running source project while its data is copied.
Export first and confirm with the user before destroy, copy/move with
volumes, or volume removal.

## GitOps

```bash
arcane-cli gitops list
arcane-cli gitops create
arcane-cli gitops status <sync>
arcane-cli gitops sync <sync>            # trigger a sync now
arcane-cli gitops files <sync>
```

For syncs that push project files back to Git, `gitops preview <sync>`
shows what the next push would change, `gitops history <sync>` lists the
Git revisions, and `gitops resolve <sync>` handles a push conflict.

Add `--agent <id>` to any command to act on one specific host.
