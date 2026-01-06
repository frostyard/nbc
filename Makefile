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

test: test-unit test-integration ## Run all tests (unit then integration)

test-unit: ## Run unit tests (no root required)
	@echo "Running unit tests..."
	@go test -v ./pkg/... -run "^Test[^I]" -skip "Integration"

test-integration: ## Run integration tests (requires root)
	@echo "Running integration tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-integration; \
	else \
		$(MAKE) _test-integration; \
	fi

_test-integration: ## Internal target for integration tests
	@go test -v ./pkg/... -run "^TestIntegration_" -timeout 10m

test-install: ## Run installation tests (requires root)
	@echo "Running installation tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-install; \
	else \
		$(MAKE) _test-install; \
	fi

_test-install: ## Internal target for install tests
	@go test -v ./pkg/... -run "^(TestBootcInstaller)" -timeout 20m

test-update: ## Run update tests (requires root)
	@echo "Running update tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-update; \
	else \
		$(MAKE) _test-update; \
	fi

_test-update: ## Internal target for update tests
	@go test -v ./pkg/... -run "^(TestSystemUpdater)" -timeout 20m

test-incus: ## Run Incus VM integration tests (requires root and incus)
	@echo "Running Incus integration tests (requires root and incus)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving environment..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-incus; \
	else \
		$(MAKE) _test-incus; \
	fi

_test-incus: ## Internal target for Incus tests
	@./test_incus.sh

test-incus-quick: ## Run quick Incus install test (requires root and incus)
	@echo "Running quick Incus install test..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-incus-quick; \
	else \
		$(MAKE) _test-incus-quick; \
	fi

_test-incus-quick: ## Internal target for quick Incus tests
	@./test_incus_quick.sh

test-incus-encryption: ## Run Incus encryption tests (requires root and incus)
	@echo "Running Incus encryption tests..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-incus-encryption; \
	else \
		$(MAKE) _test-incus-encryption; \
	fi

_test-incus-encryption: ## Internal target for Incus encryption tests
	@./test_incus_encryption.sh

test-incus-loopback: ## Run Incus loopback installation test (requires root and incus)
	@echo "Running Incus loopback tests..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-incus-loopback; \
	else \
		$(MAKE) _test-incus-loopback; \
	fi

_test-incus-loopback: ## Internal target for Incus loopback tests
	@./test_incus_loopback.sh

test-incus-all: ## Run all Incus tests (shortest to longest)
	@echo "Running all Incus tests..."
	@$(MAKE) test-incus-quick
	@$(MAKE) test-incus-loopback
	@$(MAKE) test-incus-encryption
	@$(MAKE) test-incus

test-all: ## Run all tests (unit + integration, requires root)
	@echo "Running all tests..."
	@$(MAKE) test-unit
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running integration tests with sudo..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-integration; \
	else \
		$(MAKE) _test-integration; \
	fi

test-coverage: ## Run tests with coverage report (requires root for full coverage)
	@echo "Running tests with coverage..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo and preserving PATH..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" $(MAKE) _test-coverage; \
	else \
		$(MAKE) _test-coverage; \
	fi

_test-coverage: ## Internal target for coverage tests
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

docs: build
	@echo "Generating documentation..."
	@./$(BINARY_NAME) gendocs --output docs/cli

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

clean-volumes: ## Clean up test volumes (requires root)
	@echo "Cleaning up test volumes..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		sudo -E env "PATH=$$PATH" $(MAKE) _clean-volumes; \
	else \
		$(MAKE) _clean-volumes; \
	fi

_clean-volumes: ## Internal target for cleaning volumes
	@incus storage volume list default --format csv | grep -E '^(custom|block),(nbc-|phukit-)' | cut -d',' -f2 | xargs -I{} incus storage volume rm default {}

.DEFAULT_GOAL := help
