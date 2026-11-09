BINARY  := google-play-publisher
MODULE  := github.com/vyachin/google-play-publisher

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build test test-race test-scripts lint fmt cover snapshot clean

all: build

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/google-play-publisher

test:
	go test ./...
	bash scripts/setup_test.sh

test-race:
	go test -race ./...

test-scripts:
	bash scripts/setup_test.sh

lint:
	golangci-lint run
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck scripts/*.sh; else echo "shellcheck not installed: skipping shell tests"; fi
	@if command -v actionlint >/dev/null 2>&1; then actionlint; else echo "actionlint not installed: skipping workflow lint"; fi

fmt:
	gofumpt -l -w .
	go mod tidy

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist coverage.out
