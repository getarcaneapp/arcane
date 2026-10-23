import 'settings.just'

set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list lint

all: js go proto

# Frontend and Playwright tests
js: frontend tests

# Type check and lint the frontend
frontend:
    vp -C frontend run check
    vp -C frontend run lint

# Type check Playwright tests
tests:
    vp -C tests run check

# Backend, CLI, and types modules
go: backend cli types

backend:
    cd backend && golangci-lint run -c ../.golangci.yml ./...

cli:
    cd cli && golangci-lint run -c ../.golangci.yml ./...

types:
    cd types && golangci-lint run -c ../.golangci.yml ./...

# Edge tunnel protobuf definitions
proto:
    GOBIN="$PWD/backend/.bin" go install github.com/bufbuild/buf/cmd/buf@latest
    cd {{ edge_proto_dir }} && ../.bin/buf lint
