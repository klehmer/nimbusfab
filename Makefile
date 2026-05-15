.PHONY: build cli server test lint fmt tidy clean

GO       ?= go
BIN_DIR  ?= bin
PKG      := github.com/kratus8990/cloud-infra-manager
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

LDFLAGS := -ldflags "-X $(PKG)/internal/version.Version=$(VERSION)"

build: cli server

cli:
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/mytool ./cmd/cli

server:
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/mytool-server ./cmd/server

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...
	gofmt -l . | tee /dev/stderr | (! read)

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR)
