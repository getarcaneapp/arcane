<div align="center">

  <img src="../.github/assets/img/PNG-3.png" alt="Arcane Logo" width="500" />
  <p>The Official Command Line Client</p>

<a href="https://pkg.go.dev/github.com/getarcaneapp/arcane/cli"><img src="https://pkg.go.dev/badge/github.com/getarcaneapp/arcane/cli.svg" alt="Go Reference"></a>
<a href="https://goreportcard.com/report/github.com/getarcaneapp/arcane/cli"><img src="https://goreportcard.com/badge/github.com/getarcaneapp/arcane/cli" alt="Go Report Card"></a>
<a href="https://github.com/getarcaneapp/cli/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue.svg" alt="License"></a>

<br />

</div>

## Install

This module lives inside the main Arcane repo. To build the CLI locally:

- `go install github.com/getarcaneapp/arcane/cli@latest`

## Versions

CLI versions are tagged as `cli/vX.Y.Z` in the main repo. Use module tags to pin a specific release:

- `go install github.com/getarcaneapp/arcane/cli@cli/vX.Y.Z`

## Configure

The CLI stores config in `~/.config/arcanecli.yml`.

Create a starter config file (all supported keys):

- `arcane config init`
- `arcane config backup` (moves current config to `~/.config/arcanecli.yml.bak`)

Set the Arcane server URL:

- `arcane config set server-url http://localhost:3552`

### Authenticate (choose one)

#### Option A: Device code (OIDC must be enabled for this method, as it uses your external provider.)

- `arcane auth login`

#### Option B: API key

- `arcane config set api-key arc_xxxxxxxxxxxxx`

## Useful Global Flags

- `--output text|json` for output mode (`--json` is an alias for `--output json`)
- `--env <id>` to override the configured default environment for one command
- `--yes` to auto-confirm destructive prompts
- `--no-color` to disable ANSI color output
- `--request-timeout <duration>` to override HTTP timeout per command

## Utilities

- `arcane completion bash|zsh|fish|powershell` to generate shell completions
- `arcane doctor` to run local CLI diagnostics

## Pagination Config

Set global and per-resource list limits in config:

```yaml
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

CLI precedence is:
1. `--limit`
2. `pagination.resources.<resource>.limit`
3. `pagination.default.limit`
4. command built-in default

You can configure limits with:
- `arcane config set default-limit 25`
- `arcane config set pagination.resources.containers.limit 50 pagination.resources.images.limit 100`

Legacy flag syntax remains supported:
- `arcane config set --default-limit 25`
- `arcane config set --resource-limit containers=50 --resource-limit images=100`
