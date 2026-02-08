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

Set the Arcane server URL:

- `arcane config set --server-url http://localhost:3552`

### Authenticate (choose one)

#### Option A: Device code (OIDC must be enabled for this method, as it uses your external provider.)

- `arcane auth login`

#### Option B: API key

- `arcane config set --api-key arc_xxxxxxxxxxxxx`
