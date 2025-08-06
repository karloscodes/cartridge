# Cartridge Web Library Makefile

.PHONY: help build test clean lint format deps example

# Default target
help:
	@echo "Cartridge Web Library - Available targets:"
	@echo "  build    - Build the library and examples"
	@echo "  test     - Run all tests"
	@echo "  clean    - Clean build artifacts"
	@echo "  lint     - Run linters"
	@echo "  format   - Format code"
	@echo "  deps     - Download dependencies"
	@echo "  example  - Run basic example"

# Build the library
build:
	@echo "Building Cartridge library..."
	go build ./...

# Run tests
test:
	@echo "Running tests..."
	go test ./... -v

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html
	go clean ./...

# Run linters
lint:
	@echo "Running linters..."
	go vet ./...
	go fmt ./...

# Format code
format:
	@echo "Formatting code..."
	go fmt ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Run basic example
example:
	@echo "Running basic example..."
	go run examples/basic/main.go

# Install development tools
dev-tools:
	@echo "Installing development tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run comprehensive linting
lint-full: dev-tools
	@echo "Running comprehensive linting..."
	golangci-lint run

# Setup project for development
setup: deps dev-tools
	@echo "Setting up project for development..."
	mkdir -p data logs static templates
	@echo "Project setup complete!"

# Build for production
build-prod:
	@echo "Building for production..."
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o cartridge-linux ./examples/basic

# Run security scan
security:
	@echo "Running security scan..."
	go list -json -m all | nancy sleuth

# Generate documentation
docs:
	@echo "Generating documentation..."
	go doc -all ./... > API.md

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Race condition detection
race:
	@echo "Running race condition detection..."
	go test -race ./...

# Initialize new project with Cartridge
init-project:
	@echo "Initializing new Cartridge project..."
	@read -p "Enter project name: " name; \
	mkdir -p $$name; \
	cd $$name; \
	go mod init $$name; \
	echo "module $$name\n\ngo 1.21\n\nrequire github.com/karloscodes/cartridge v0.1.0" > go.mod; \
	mkdir -p cmd static templates data logs; \
	echo "Project $$name initialized!"
