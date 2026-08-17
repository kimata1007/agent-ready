.PHONY: build test vet

build:
	go build -trimpath -o bin/agent-ready ./cmd/agent-ready

test:
	go test -race -coverprofile=coverage.out ./...

vet:
	go vet ./...
