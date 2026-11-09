BINARY  := google-play-publisher
MODULE  := github.com/vyachin/google-play-publisher

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build test test-race lint fmt cover snapshot clean

all: build

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/google-play-publisher

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	golangci-lint run

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
