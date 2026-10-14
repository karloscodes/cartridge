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
	@echo "  example  - Run the notes example in development"

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

# Run the notes example in development
example:
	@echo "Running the notes example on http://127.0.0.1:8080 ..."
	cd examples/notes && NOTES_ENV=development go run .

# Install development tools
dev-tools:
	@echo "Installing development tools..."
	go install golang.org/x/tools/cmd/goimports@v0.52.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6

# Run comprehensive linting
lint-full: dev-tools
	@echo "Running comprehensive linting..."
	golangci-lint run

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Race condition detection
race:
	@echo "Running race condition detection..."
	go test -race ./...

# Scan for known reachable vulnerabilities (CVEs)
.PHONY: vuln
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
