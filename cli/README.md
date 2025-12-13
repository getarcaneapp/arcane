<div align="center">

  <img src="../.github/assets/IMG/PNG-3.png" alt="Arcane Logo" width="500" />
  <p>The Official Command Line Client</p>

<a href="https://pkg.go.dev/go.getarcane.app/cli"><img src="https://pkg.go.dev/badge/go.getarcane.app/cli.svg" alt="Go Reference"></a>
<a href="https://goreportcard.com/report/go.getarcane.app/cli"><img src="https://goreportcard.com/badge/go.getarcane.app/cli" alt="Go Report Card"></a>
<a href="https://github.com/getarcaneapp/cli/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue.svg" alt="License"></a>

<br />

</div>

## Install

This module lives inside the main Arcane repo. To build the CLI locally:

- `go install go.getarcane.app/cli@latest`

## Configure

The CLI stores config in `~/.config/arcanecli.yml`.

Set the Arcane server URL:

- `arcane config set --server-url http://localhost:3552`

### Authenticate (choose one)

#### Option A: JWT login

- `arcane auth login`

#### Option B: API key

- `arcane config set --api-key arc_xxxxxxxxxxxxx`
