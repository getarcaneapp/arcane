set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list format

# Format every workspace
[arg('check', long, value='--check')]
[parallel]
all check='': (js check) (go check) (just check)

# All JavaScript and TypeScript in the repository
[arg('check', long, value='--check')]
js check='':
    vp fmt {{ check }} .

[arg('check', long, value='--check')]
frontend check='':
    vp fmt {{ check }} frontend

[arg('check', long, value='--check')]
tests check='':
    vp fmt {{ check }} tests

# Go modules with the formatters configured in .golangci.yml
[arg('check', long, value='--check')]
go check='':
    cd backend && golangci-lint fmt -c ../.golangci.yml {{ if check != '' { '--diff' } else { '' } }} ./...
    cd cli && golangci-lint fmt -c ../.golangci.yml {{ if check != '' { '--diff' } else { '' } }} ./...
    cd types && golangci-lint fmt -c ../.golangci.yml {{ if check != '' { '--diff' } else { '' } }} ./...

# Root Justfile and its modules
[arg('check', long, value='--check')]
[script('bash')]
just check='':
    set -euo pipefail
    for file in Justfile just/*.just just/*/*.just; do
        just --fmt {{ check }} --justfile "$file"
    done
