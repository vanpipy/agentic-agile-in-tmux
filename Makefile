# awp — Makefile for releases
#
# Common targets:
#   make build         — local debug build
#   make release VERSION=x.y.z   — full release pipeline (validate, tag, push, build)
#   make release-local VERSION=x.y.z — same as release but skips the git push
#   make test          — run all tests
#   make lint          — go vet + race detector
#   make clean         — remove binaries
#
# Phase 5: phase 5 polish tooling.

VERSION ?= 0.0.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
  -X github.com/pi/awp/internal/buildinfo.version=$(VERSION) \
  -X github.com/pi/awp/internal/buildinfo.commit=$(COMMIT) \
  -X github.com/pi/awp/internal/buildinfo.buildDate=$(DATE)

.PHONY: build release release-local test test-integration test-e2e lint clean install run

build:
	go build -o awp .

# release: full pipeline — validate semver, ensure clean tree, ensure on main,
# ensure tag not present, create annotated tag, push tag, build binary with
# ldflags-injected version metadata.
#
# VERSION is REQUIRED. Use `make release-local VERSION=x.y.z` if you cannot push.
#
# All pre-flight checks live in cmd/releasecheck so they're unit-tested in Go
# (see internal/release + cmd/releasecheck tests) and not duplicated in shell.
release:
	@if [ "$(VERSION)" = "0.0.0-dev" ] || [ -z "$(VERSION)" ]; then \
		echo "ERROR: VERSION is required, e.g. make release VERSION=0.1.0" >&2; \
		exit 1; \
	fi
	@echo "Pre-flight: validating $(VERSION) ..."
	@go run ./cmd/releasecheck $(VERSION) main
	@echo "Tagging v$(VERSION) ..."
	@git tag -a "v$(VERSION)" -m "awp $(VERSION)"
	@echo "Pushing v$(VERSION) to origin ..."
	@git push origin "v$(VERSION)"
	@echo "Building v$(VERSION) ..."
	@go build -ldflags="$(LDFLAGS)" -o awp .
	@echo "Release v$(VERSION) complete."

# release-local: same as release but stops before `git push`. Useful for
# offline releases, CI dry-runs, and verifying the pipeline locally.
release-local:
	@if [ "$(VERSION)" = "0.0.0-dev" ] || [ -z "$(VERSION)" ]; then \
		echo "ERROR: VERSION is required, e.g. make release-local VERSION=0.1.0" >&2; \
		exit 1; \
	fi
	@echo "Pre-flight: validating $(VERSION) ..."
	@go run ./cmd/releasecheck $(VERSION) main
	@echo "Tagging v$(VERSION) (local only) ..."
	@git tag -a "v$(VERSION)" -m "awp $(VERSION)"
	@echo "Building v$(VERSION) ..."
	@go build -ldflags="$(LDFLAGS)" -o awp .
	@echo "Local release v$(VERSION) complete (not pushed)."
	@echo "Delete local tag with: git tag -d v$(VERSION)"

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
