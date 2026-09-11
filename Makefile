# Convenience targets for local agtk builds. Release builds go through
# .goreleaser.yaml on tag push (see .github/workflows/release.yml).

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/version.Version=$(VERSION)

.PHONY: build install test fmt vet check logo

build:
	go build -ldflags "$(LDFLAGS)" -o bin/agtk ./source/toolkit/cmd/agtk

install:
	go install -ldflags "$(LDFLAGS)" ./source/toolkit/cmd/agtk

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

# The white plate is injected ahead of the artwork rather than kept in a second
# SVG, so the mark's paths have one copy and the two cannot drift. Needs
# rsvg-convert (brew install librsvg).
logo:
	sed 's|<title>|<rect width="1024" height="1024" rx="230" fill="#FFFFFF"/><title>|' \
	  assets/agtk-bot-mark.svg | rsvg-convert -w 1024 -h 1024 -o assets/agtk-bot-logo.png
