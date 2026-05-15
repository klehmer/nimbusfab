.PHONY: build cli server test lint fmt tidy clean schemagen generate

GO       ?= go
BIN_DIR  ?= bin
PKG      := github.com/klehmer/nimbusfab
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

LDFLAGS := -ldflags "-X $(PKG)/internal/version.Version=$(VERSION)"

build: cli server

cli:
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/nimbusfab ./cmd/cli

server:
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/nimbusfab-server ./cmd/server

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...
	gofmt -l . | tee /dev/stderr | (! read)

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

schemagen:
	$(GO) run ./tools/schemagen/

generate: schemagen

clean:
	rm -rf $(BIN_DIR)
