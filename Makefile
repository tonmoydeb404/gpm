GPM_HOME ?= $(abspath .gpm-dev)

.PHONY: run tui build install test vet fmt clean

# Dev runner: `make run ARGS="profile list"` executes in a sandbox.
run:
	GPM_HOME=$(GPM_HOME) go run ./cmd/gpm $(ARGS)

# Interactive TUI in the sandbox.
tui:
	GPM_HOME=$(GPM_HOME) go run ./cmd/gpm

build:
	go build -o bin/gpm ./cmd/gpm

install:
	go install ./cmd/gpm

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin .gpm-dev
