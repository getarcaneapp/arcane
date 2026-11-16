set working-directory := '../..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list build::single

all: frontend backend

frontend:
    vp -C frontend run build

backend:
    cd backend && go build ./...

# Cross-compile and vet the backend for windows/amd64
windows:
    cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags exclude_frontend -o /dev/null ./cmd/main.go
    cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet -tags exclude_frontend ./...
