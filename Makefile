# Console — common developer tasks.
BINARY := console
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet fmt run clean install tidy

build: ## Build the console binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/console

test: ## Run all tests
	go test $(PKG)

vet: ## Run go vet
	go vet $(PKG)

fmt: ## Format all Go files
	gofmt -w cmd internal

run: build ## Build and run the server
	./$(BINARY) serve

tidy: ## Tidy module dependencies
	go mod tidy

clean: ## Remove build artifacts and local databases
	rm -f $(BINARY) *.db *.db-shm *.db-wal

install: ## Install the binary into GOBIN
	go install -ldflags "$(LDFLAGS)" ./cmd/console
