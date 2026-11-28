---
name: arcane-cli
description: >-
  Use the arcane command-line client to manage Docker hosts through an
  Arcane server: install, configure server URL and API key or device-code
  login, global flags (--output json, --agent, --yes), and everyday
  container, image, volume, network, image-vulnerability, updater and system
  commands. Use when scripting Docker management against Arcane.
---

# Arcane CLI

References: `cli/README.md`, `cli/pkg/`.

## Install and connect

```bash
go install getarcane.dev/cli/v3@latest            # or @cli/vX.Y.Z to pin
arcane config init                                 # writes ~/.config/arcanecli.yml
arcane config set server-url https://arcane.example.com
arcane config set api-key arc_xxx                  # or: arcane auth login (device code, needs OIDC)
arcane auth me
arcane doctor
```

Create keys with `arcane auth api-keys create <name>`. Don't echo keys
back to the user or commit `arcanecli.yml`.

## Global flags

- `--output json` (or `--json`) for machine-readable output
- `--agent <id>` to target one remote agent (`arcane agents list`)
- `--yes` auto-confirms destructive prompts; only use it when the user
  asked for that exact action
- `--request-timeout <duration>`

## Everyday commands

```bash
arcane containers list                      # aliases: container, c
arcane containers get|start|stop|restart|redeploy <name>
arcane containers delete <name>             # destructive
arcane images list | pull <image> | prune | search <term>
arcane images build <context-dir|git-url>
arcane volumes list | get <name>
arcane volumes create --name data --driver local
arcane volumes backups list|create <volume>
arcane volumes backups restore <volume> [backup-id]   # overwrites volume data
arcane networks list
arcane images vulnerabilities scan <image>
arcane images vulnerabilities image <image> | list | summary
arcane updater status | run | history
arcane system info | prune
arcane system convert "docker run -p 80:80 nginx"   # docker run -> compose
```

Run `arcane <group> --help` for exact flags; list commands support
pagination limits configured under `pagination:` in the config file.

## Safety

Prune, delete, destroy and restore remove or overwrite data. List what
will be affected first and confirm with the user before running them.
