set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list test

all: e2e backend cli

# Playwright E2E tests
e2e:
    vp -C tests run test

backend: (_go 'backend' '-tags=exclude_frontend,buildables' '-ldflags=-X github.com/getarcaneapp/arcane/backend/v2/buildables.EnabledFeatures=autologin')

cli: (_go 'cli')

[positional-arguments]
[script('bash')]
_go module *flags:
    set -euo pipefail
    shift
    cd "{{ module }}"
    if [ -n "${GO_JUNIT_REPORT_FILE:-}" ]; then
        mkdir -p "$(dirname "$GO_JUNIT_REPORT_FILE")"
        go test -json "$@" ./... -race -coverprofile=coverage.txt -covermode=atomic -v 2>&1 | go run github.com/jstemmer/go-junit-report/v2@v2.1.0 -parser gojson -set-exit-code -out "$GO_JUNIT_REPORT_FILE"
    else
        go test "$@" ./... -race -coverprofile=coverage.txt -covermode=atomic -v
    fi
