.PHONY: build run clean test

BINARY := tokenomics
BUILD_DIR := ./bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/tokenomics

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
