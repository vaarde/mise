.PHONY: build run test lint clean

# Build the mise binary
build:
	go build -o bin/mise .

# Run mise directly (pass ARGS, e.g. make run ARGS="plan")
run:
	go run . $(ARGS)

# Run all tests
test:
	go test -race ./...

# Run tests with verbose output
test-v:
	go test -race -v ./...

# Run linter (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

# Format all Go files
fmt:
	gofmt -s -w .

# Remove build artifacts
clean:
	rm -rf bin/
	rm -f mise
