# AWP — Makefile for releases
#
# Common targets:
#   make build         — local debug build
#   make release       — tagged build with ldflags
#   make test          — run all tests
#   make lint          — go vet + race detector
#   make clean         — remove binaries

VERSION ?= 0.0.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
  -X github.com/vanpiy/awp/internal/buildinfo.Version=$(VERSION) \
  -X github.com/vanpiy/awp/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/vanpiy/awp/internal/buildinfo.BuildDate=$(DATE)

.PHONY: build release test lint install clean

build:
	go build -o awp .

release:
	go build -ldflags="$(LDFLAGS)" -o awp .

test:
	go test ./...

lint:
	go vet ./...
	go test -race ./internal/...

install: build
	install -m 0755 awp $(GOPATH)/bin/awp

clean:
	rm -f awp
	rm -f *.test
