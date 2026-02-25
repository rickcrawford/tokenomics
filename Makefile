.PHONY: build run clean test lint tidy

BINARY := tokenomics
BUILD_DIR := ./bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -X github.com/rickcrawford/tokenomics/cmd.Version=$(VERSION) \
           -X github.com/rickcrawford/tokenomics/cmd.Commit=$(COMMIT) \
           -X github.com/rickcrawford/tokenomics/cmd.BuildDate=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .

run: build
	$(BUILD_DIR)/$(BINARY) serve

clean:
	rm -rf $(BUILD_DIR) certs tokenomics.db

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
