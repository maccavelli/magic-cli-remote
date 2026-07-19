MODULE  := github.com/maccavelli/magic-cli-remote
BIN     := bin/mcremote
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build test race run fmt vet tidy clean

build:
	@mkdir -p bin
	go build $(LDFLAGS) -o $(BIN) ./cmd/mcremote

test:
	go test ./...

race:
	go test -race ./...

run:
	go run $(LDFLAGS) ./cmd/mcremote serve

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
