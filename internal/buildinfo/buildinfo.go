// Package buildinfo — version metadata injected at build time.
//
// Phase 5: polished `awp --version` output. Set via -ldflags:
//
//	go build -ldflags="-X github.com/pi/awp/internal/buildinfo.Version=0.1.0 \
//	                   -X github.com/pi/awp/internal/buildinfo.Commit=abc1234 \
//	                   -X github.com/pi/awp/internal/buildinfo.BuildDate=2026-06-17" \
//	        -o awp .
//
// Or use the Makefile target `make release`.
package buildinfo

import (
	"fmt"
	"runtime"
)

// These are set at build time via -ldflags.
var (
	// Version is the semver string, e.g. "0.1.0".
	Version = "0.0.0-dev"

	// Commit is the git short SHA.
	Commit = "dev"

	// BuildDate is the ISO 8601 build timestamp.
	BuildDate = "unknown"
)

// String returns a multi-line version string suitable for
// `awp --version` output.
func String() string {
	return fmt.Sprintf(
		"awp %s\ncommit:  %s\nbuilt:   %s\ngo:      %s",
		Version, Commit, BuildDate, runtime.Version(),
	)
}

// Short returns a one-line version string.
func Short() string {
	return fmt.Sprintf("awp %s (%s, %s)", Version, Commit, BuildDate)
}
