---
name: arcane-cli
description: >-
  Use the arcane-cli command-line client to manage Docker hosts through an
  Arcane server: install, configure server URL and API key or device-code
  login, global flags (--output json, --agent, --yes), and everyday
  container, image, volume, network, image-vulnerability, updater and system
  commands. Use when scripting Docker management against Arcane.
---

# Arcane CLI

The remote client is `arcane-cli`. The `arcane` binary in the server image
runs the server and has no `config`, `containers` or `projects` commands.

Reference: [`cli/`](https://github.com/getarcaneapp/arcane/tree/main/cli) in the Arcane repo; run `arcane-cli <group> --help` for exact flags.

## Install and connect

```bash
# Release assets and the arcane-cli deb/rpm/apk packages install `arcane-cli`.
# Building from source names the binary `cli`; rename it:
go install getarcane.dev/cli/v3@latest && mv "$(go env GOPATH)/bin/cli" "$(go env GOPATH)/bin/arcane-cli"
arcane-cli config init                                 # writes ~/.config/arcanecli.yml
arcane-cli config set server-url https://arcane.example.com
arcane-cli config set api-key arc_xxx                  # or: arcane-cli auth login (device code, needs OIDC)
arcane-cli auth me
arcane-cli doctor
```

Create keys with `arcane-cli auth api-keys create <name>`. Don't echo keys
back to the user or commit `arcanecli.yml`.

## Global flags

- `--output json` (or `--json`) for machine-readable output
- `--agent <id>` to target one remote agent (`arcane-cli agents list`)
- `--yes` auto-confirms destructive prompts; only use it when the user
  asked for that exact action
- `--request-timeout <duration>`

## Everyday commands

```bash
arcane-cli containers list                      # aliases: container, c
arcane-cli containers get|start|stop|restart|redeploy <name>
arcane-cli containers delete <name>             # destructive
arcane-cli images list | pull <image> | prune | search <term>
arcane-cli images build <context-dir|git-url>
arcane-cli volumes list | get <name>
arcane-cli volumes create --name data --driver local
arcane-cli volumes backups list|create <volume>
arcane-cli volumes backups restore <volume> [backup-id]   # overwrites volume data
arcane-cli networks list
arcane-cli images vulnerabilities scan <image>
arcane-cli images vulnerabilities image <image> | list | summary
arcane-cli updater status | run | history
arcane-cli system info | prune
arcane-cli system convert "docker run -p 80:80 nginx"   # docker run -> compose
```

Run `arcane-cli <group> --help` for exact flags; list commands support
pagination limits configured under `pagination:` in the config file.

## Safety

Prune, delete, destroy and restore remove or overwrite data. List what
will be affected first and confirm with the user before running them.
