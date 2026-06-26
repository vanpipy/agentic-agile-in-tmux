# awp — Makefile for releases
#
# Common targets:
#   make build         — local debug build
#   make release       — tagged build with ldflags
#   make test          — run all tests
#   make lint          — go vet + race detector
#   make clean         — remove binaries
#
# Phase 5: phase 5 polish tooling.

VERSION ?= 0.0.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
  -X github.com/pi/awp/internal/buildinfo.Version=$(VERSION) \
  -X github.com/pi/awp/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/pi/awp/internal/buildinfo.BuildDate=$(DATE)

.PHONY: build release test test-integration test-e2e lint clean install run

build:
	go build -o awp .

release:
	go build -ldflags="$(LDFLAGS)" -o awp .

test:
	go test ./...

# Integration tests: test/{sub}/, mirrors internal/ structure.
# Build tag //go:build integration. Uses mock pi binary.
test-integration:
	go test -tags integration -count=1 ./test/...

# End-to-end tests: e2e/, flat. Build tag //go:build e2e.
# Requires real `awp` and `pi` binaries in PATH.
test-e2e:
	go test -tags e2e -count=1 ./e2e/...

lint:
	go vet ./...
	go test -race ./internal/...

install: build
	install -m 0755 awp $(GOPATH)/bin/awp

run: build
	./awp

clean:
	rm -f awp
	rm -f *.test
	find . -name 'integration.test' -not -path './.git/*' -delete 2>/dev/null || true
