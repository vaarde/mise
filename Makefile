.PHONY: build install run test test-v test-race test-integration cover lint fmt clean

# Windows needs the .exe suffix, or the binary cannot be executed.
BINARY := mise
ifeq ($(OS),Windows_NT)
	BINARY := mise.exe
endif

VERSION   ?= 0.1.0-dev
COMMIT    := $(shell git rev-parse --short=12 HEAD 2>/dev/null)
BUILDDATE := $(shell git log -1 --format=%cI 2>/dev/null)
PKG       := github.com/vaarde/mise/internal/version

LDFLAGS := -X $(PKG).Version=$(VERSION) \
           -X $(PKG).Commit=$(COMMIT) \
           -X $(PKG).BuildDate=$(BUILDDATE)

# Build the mise binary
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

# Install to GOPATH/bin, so `mise` works from any directory
install:
	go install -ldflags "$(LDFLAGS)" .

# Run mise directly (pass ARGS, e.g. make run ARGS="plan")
run:
	go run -ldflags "$(LDFLAGS)" . $(ARGS)

# Run all unit tests. No network, no credentials needed.
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Race detection needs cgo and a C compiler, which not every dev machine
# has. CI runs this; locally, `make test` is the one that always works.
test-race:
	go test -race ./...

# Run the integration suite against a real Square sandbox account.
#
# Needs SQUARE_ACCESS_TOKEN set to a *sandbox* token — these tests create,
# modify, and delete catalog objects. Without one they skip rather than
# fail, so this target is safe to run unconfigured.
test-integration:
	go test -tags integration ./cmd/ -run Integration -v -timeout 10m

# Per-package coverage summary
cover:
	go test -cover ./...

# Run linter (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

# Format all Go files
fmt:
	gofmt -s -w .

# Remove build artifacts
clean:
	rm -rf bin/
	rm -f mise mise.exe
