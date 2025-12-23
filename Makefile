.PHONY: build clean install test run help

BINARY_NAME=nbc
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -s -w"

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME) version $(VERSION)..."
	@go build $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "Build complete: ./$(BINARY_NAME)"

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@go clean

install: build ## Install to /usr/local/bin
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installed successfully"

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

test-unit: ## Run unit tests (no root required)
	@echo "Running unit tests..."
	@go test -v ./pkg/... -run "^Test[^I]" -skip "Integration"

test-integration: ## Run integration tests (requires root)
	@echo "Running integration tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E PATH="/usr/sbin:/sbin:$$PATH" $(MAKE) _test-integration; \
	else \
		$(MAKE) _test-integration; \
	fi

_test-integration: ## Internal target for integration tests
	@go test -v ./pkg/... -run "^TestIntegration_" -timeout 10m

test-install: ## Run installation tests (requires root)
	@echo "Running installation tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E PATH="/usr/sbin:/sbin:$$PATH" $(MAKE) test-install; \
	else \
		go test -v ./pkg/... -run "^(TestBootcInstaller)" -timeout 20m; \
	fi

test-update: ## Run update tests (requires root)
	@echo "Running update tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E PATH="/usr/sbin:/sbin:$$PATH" $(MAKE) test-update; \
	else \
		go test -v ./pkg/... -run "^(TestSystemUpdater)" -timeout 20m; \
	fi

test-incus: ## Run Incus VM integration tests (requires root and incus)
	@echo "Running Incus integration tests (requires root and incus)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving environment..."; \
		sudo -E env "PATH=$$PATH" $(MAKE) test-incus; \
	else \
		./test_incus.sh; \
	fi

test-all: ## Run all tests (unit + integration, requires root)
	@echo "Running all tests..."
	@$(MAKE) test-unit
	@$(MAKE) test-integration

test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	@go test -v ./pkg/... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...

lint: ## Run linter
	@echo "Running linter..."
	@golangci-lint run || echo "golangci-lint not installed, skipping"

run: build ## Build and run
	@./$(BINARY_NAME)

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@podman build -t $(BINARY_NAME):$(VERSION) .
	@podman tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

bump: ## generate a new version with svu
	@$(MAKE) build
	@$(MAKE) test
	@$(MAKE) fmt
	$(MAKE) lint
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working directory is not clean. Please commit or stash changes before bumping version."; \
		exit 1; \
	fi
	@echo "Creating new tag..."
	@version=$$(svu next); \
		git tag -a $$version -m "Version $$version"; \
		echo "Tagged version $$version"; \
		echo "Pushing tag $$version to origin..."; \
		git push origin $$version

clean-volumes:
	@echo "Cleaning up test volumes..."
	@incus storage volume list default --format csv | grep -E '^(custom|block),(nbc-|phukit-)' | cut -d',' -f2 | xargs -I{} incus storage volume rm default {}

.DEFAULT_GOAL := help
