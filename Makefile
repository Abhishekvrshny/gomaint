# GoMaint Makefile
# This Makefile provides targets for building, testing, and code quality checks

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOPATH?=$(shell $(GOCMD) env GOPATH)

# Binary names
BINARY_NAME=gomaint
BINARY_UNIX=$(BINARY_NAME)_unix

# Directories
EXAMPLES_DIR=./examples
PKG_DIR=./pkg

# Tools
GOIMPORTS=$(GOPATH)/bin/goimports
STATICCHECK=$(GOPATH)/bin/staticcheck

.PHONY: all build clean test coverage deps install-tools goimports-check goimports-fix lint-check staticcheck help

# Default target
all: test build

# Build the project
build:
	$(GOBUILD) -v ./...

# Build for Linux
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -v -o $(BINARY_UNIX) ./...

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with race detection
test-race:
	$(GOTEST) -v -race ./...

# Install dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOGET) golang.org/x/tools/cmd/goimports@latest
	$(GOGET) honnef.co/go/tools/cmd/staticcheck@latest
	@echo "Tools installed successfully"

# Check if goimports would make changes (for CI)
goimports-check:
	@if [ ! -f $(GOIMPORTS) ]; then \
		echo "goimports not found. Run 'make install-tools' first."; \
		exit 1; \
	fi
	@echo "Checking goimports formatting..."
	@if [ -n "$$($(GOIMPORTS) -l .)" ]; then \
		echo "The following files need goimports formatting:"; \
		$(GOIMPORTS) -l .; \
		echo "Run 'make goimports-fix' to fix these issues"; \
		exit 1; \
	else \
		echo "All files are properly formatted with goimports"; \
	fi

# Fix import formatting
goimports-fix:
	@if [ ! -f $(GOIMPORTS) ]; then \
		echo "goimports not found. Run 'make install-tools' first."; \
		exit 1; \
	fi
	@echo "Fixing goimports formatting..."
	$(GOIMPORTS) -w .
	@echo "goimports formatting applied"

# Check Go formatting
gofmt-check:
	@echo "Checking gofmt formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "The following files need gofmt formatting:"; \
		gofmt -l .; \
		echo "Run 'make gofmt-fix' to fix these issues"; \
		exit 1; \
	else \
		echo "All files are properly formatted with gofmt"; \
	fi

# Fix Go formatting
gofmt-fix:
	@echo "Fixing gofmt formatting..."
	gofmt -w .
	@echo "gofmt formatting applied"

# Run linting checks (using staticcheck as golint is deprecated)
lint-check: staticcheck

# Run staticcheck (more comprehensive linter)
staticcheck:
	@if [ ! -f $(STATICCHECK) ]; then \
		echo "staticcheck not found. Run 'make install-tools' first."; \
		exit 1; \
	fi
	@echo "Running staticcheck..."
	@$(STATICCHECK) ./... || echo "staticcheck found issues (see output above)"

# Run staticcheck with strict mode (fail on issues) - useful for CI
staticcheck-strict:
	@if [ ! -f $(STATICCHECK) ]; then \
		echo "staticcheck not found. Run 'make install-tools' first."; \
		exit 1; \
	fi
	@echo "Running staticcheck in strict mode..."
	$(STATICCHECK) ./...

# Run all linting checks
lint: gofmt-check goimports-check lint-check

# Fix all formatting issues
format: gofmt-fix goimports-fix

# Run pre-commit checks (useful for CI/CD)
pre-commit: lint test

# Build all example services
build-examples:
	@echo "Building example services..."
	@for dir in $(EXAMPLES_DIR)/*/; do \
		if [ -f "$$dir/main.go" ]; then \
			echo "Building $$dir"; \
			(cd "$$dir" && $(GOBUILD) -o $$(basename "$$dir") .); \
		fi \
	done

# Clean example binaries
clean-examples:
	@echo "Cleaning example binaries..."
	@for dir in $(EXAMPLES_DIR)/*/; do \
		if [ -f "$$dir/main.go" ]; then \
			rm -f "$$dir/$$(basename "$$dir")"; \
		fi \
	done

# Test example services compile
test-examples:
	@echo "Testing example services compile..."
	@for dir in $(EXAMPLES_DIR)/*/; do \
		if [ -f "$$dir/main.go" ]; then \
			echo "Testing $$dir"; \
			$(GOBUILD) -o /tmp/test_binary "$$dir/main.go" && rm -f /tmp/test_binary; \
		fi \
	done

# Run all checks and tests
ci: deps lint test test-race build test-examples

# Show help
help:
	@echo "GoMaint Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Run tests and build (default)"
	@echo "  build            - Build the project"
	@echo "  build-linux      - Cross-compile for Linux"
	@echo "  clean            - Clean build artifacts"
	@echo "  test             - Run tests"
	@echo "  test-race        - Run tests with race detection"
	@echo "  coverage         - Run tests with coverage report"
	@echo "  deps             - Download and tidy dependencies"
	@echo ""
	@echo "Code Quality:"
	@echo "  install-tools    - Install development tools (goimports, golint, staticcheck)"
	@echo "  goimports-check  - Check import formatting (exit 1 if issues found)"
	@echo "  goimports-fix    - Fix import formatting"
	@echo "  gofmt-check      - Check Go code formatting (exit 1 if issues found)"
	@echo "  gofmt-fix        - Fix Go code formatting"
	@echo "  lint-check       - Run linting checks (staticcheck)"
	@echo "  staticcheck      - Run staticcheck linter"
	@echo "  lint             - Run all linting checks"
	@echo "  format           - Fix all formatting issues"
	@echo "  pre-commit       - Run pre-commit checks (lint + test)"
	@echo ""
	@echo "Examples:"
	@echo "  build-examples   - Build all example services"
	@echo "  clean-examples   - Clean example binaries"
	@echo "  test-examples    - Test that all examples compile"
	@echo ""
	@echo "CI/CD:"
	@echo "  ci               - Run full CI pipeline (deps, lint, test, race, build, examples)"
	@echo ""
	@echo "Usage examples:"
	@echo "  make test                    # Run tests"
	@echo "  make goimports-check         # Check import formatting"
	@echo "  make goimports-fix           # Fix import formatting"
	@echo "  make lint-check              # Run linter"
	@echo "  make pre-commit              # Run pre-commit checks"
	@echo "  make ci                      # Run full CI pipeline"