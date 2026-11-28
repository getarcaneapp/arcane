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

Reference: `cli/pkg/projects/`, `cli/pkg/gitops/`.

## Create and inspect

```bash
arcane projects create --name web --file compose.yaml --env-file .env
arcane projects list --status running --updates has_update
arcane projects get web
arcane projects workspace cat web compose.yaml
```

## Lifecycle

```bash
arcane projects up web
arcane projects down web
arcane projects restart web
arcane projects pull web
arcane projects redeploy web             # pull images and restart
arcane projects build web --service api --push
arcane projects run-job web migrate      # streams output
```

## Updates

```bash
arcane projects updates                  # projects with newer images
arcane projects upgrade web              # all services with a newer image
arcane projects upgrade web api worker   # only these services
```

## Backup, migrate, remove

```bash
arcane projects export web web.tar.gz --all-volumes --stop   # include volume data
arcane projects import web.tar.gz
arcane projects copy web --to <agent-id> --volumes
arcane projects move web --to <agent-id> --volumes            # stops source while copying volumes
arcane projects archive web                                   # stopped projects only
arcane projects destroy web --remove-volumes                  # deletes data
```

`destroy` removes containers and, by default, the project's files
(`--remove-files`); `--remove-volumes` also deletes data. Export first and
confirm with the user before destroy, move or volume removal.

## GitOps

```bash
arcane gitops list
arcane gitops create
arcane gitops preview <sync>             # what would change
arcane gitops sync <sync>
arcane gitops status <sync> | history <sync>
```

Preview before syncing a repository change to a running host.

Add `--agent <id>` to any command to act on a remote host.
