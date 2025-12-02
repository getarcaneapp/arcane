.PHONY: fmt lint test mocks test_coverage test_ci

GO_PKGS   := $(shell go list -f {{.Dir}} ./...)

fmt:
	@go list -f {{.Dir}} ./... | xargs -I{} gofmt -w -s {}

lint:
	@grep "^func [a-zA-Z]" example_test.go | sort -c
	@golangci-lint run

test:
	@go test -race -v $(GO_FLAGS) -count=1 $(GO_PKGS)

test_coverage:
	@go test -race -v $(GO_FLAGS) -count=1 -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)

test_ci:
	@go test -race -v $(GO_FLAGS) -count=1 $(GO_PKGS)

mocks:
	@go generate ./...
