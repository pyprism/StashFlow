.PHONY: test test-verbose test-cover test-bench build build-arm64 clean help

# Default target
all: test build

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-verbose:
	go test -v ./...

# Run tests with coverage
test-cover:
	go test -cover ./...

# Generate HTML coverage report
coverage-html:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run benchmark tests
test-bench:
	go test -bench=. -benchmem ./...

# Build the binary
build:
	go build -o stashflow ./cmd/stashflow

# build for arm64
build-arm64:
	GOOS=linux GOARCH=arm64 go build -o stashflow-linux-arm64 ./cmd/stashflow

# Run linting
lint:
	go vet ./...
	go fmt ./...

# Clean build artifacts and coverage files
clean:
	rm -f stashflow stashflow-linux-arm64 coverage.out coverage.html

# Show help
help:
	@echo "Available targets:"
	@echo "  test         - Run all unit tests"
	@echo "  test-verbose - Run tests with verbose output"
	@echo "  test-cover   - Run tests with coverage"
	@echo "  coverage-html - Generate HTML coverage report"
	@echo "  test-bench   - Run benchmark tests"
	@echo "  build        - Build the stashflow binary"
	@echo "  build-arm64  - Build for Linux ARM64"
	@echo "  lint         - Run go vet and go fmt"
	@echo "  clean        - Clean build artifacts"
	@echo "  help         - Show this help message"
