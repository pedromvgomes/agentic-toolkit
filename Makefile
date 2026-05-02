# Convenience targets for local agtk builds. Release builds go through
# .goreleaser.yaml on tag push (see .github/workflows/release.yml).

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/pedromvgomes/agentic-toolkit/internal/version.Version=$(VERSION)

.PHONY: build install test fmt vet check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/agtk ./cmd/agtk

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/agtk

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet test
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
	  echo "gofmt -s -l . is not clean:"; echo "$$unformatted"; exit 1; \
	fi
