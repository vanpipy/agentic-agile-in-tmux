// Package buildinfo — version metadata injected at build time.
//
// Two injection paths, one resolver:
//
//  1. Release builds (make release):
//     `go build -ldflags="-X .../buildinfo.version=$VERSION \
//                        -X .../buildinfo.commit=$COMMIT \
//                        -X .../buildinfo.buildDate=$DATE"`
//
//  2. `go install pkg@vX.Y.Z` or `go run` from a git checkout:
//     No ldflags. Go's runtime/debug fills in vcs.revision / vcs.time
//     automatically and sets bi.Main.Version to the module version.
//     The Version()/Commit()/BuildDate() functions fall back to those.
//
// The lowercase vars (version, commit, buildDate) are ldflags targets.
// They start at "0.0.0-dev" / "dev" / "unknown" — the resolvers
// treat those sentinels as "no ldflags injected" and fall through to
// the BuildInfo path. See buildinfo_test.go for the fallback chain.
//
// Phase 5: polished `awp version` output. Set via -ldflags:
//
//	go build -ldflags="-X github.com/pi/awp/internal/buildinfo.version=0.1.0 \
//	                   -X github.com/pi/awp/internal/buildinfo.commit=abc1234 \
//	                   -X github.com/pi/awp/internal/buildinfo.buildDate=2026-06-17" \
//	        -o awp .
//
// Or use the Makefile target `make release`.
package buildinfo

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// These are set at build time via -ldflags (lowercase so they don't
// collide with the exported Version()/Commit()/BuildDate() resolver
// functions below). Defaults are the "no ldflags" sentinel.
var (
	// version is the semver string, e.g. "0.1.0".
	version = "0.0.0-dev"

	// commit is the git short SHA.
	commit = "dev"

	// buildDate is the ISO 8601 build timestamp.
	buildDate = "unknown"
)

// ldflagSentinels are the default values that signal "ldflags did not
// inject a real value". Resolvers treat these as fall-through.
const (
	ldSentinelVersion   = "0.0.0-dev"
	ldSentinelCommit    = "dev"
	ldSentinelBuildDate = "unknown"
)

// semverLike matches "vX.Y.Z" or "X.Y.Z" (with optional pre-release/build).
// Used to filter out "(devel)" and other Go debug.BuildInfo noise.
var semverLike = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

// --- Public resolvers (fallback chain: ldflags → BuildInfo → default) ---

// Version returns the build's logical version.
//
// Resolution order:
//  1. ldflags-injected version (when != "0.0.0-dev", i.e. release build)
//  2. debug.BuildInfo.Main.Version (when semver-like, e.g. `go install pkg@vX.Y.Z`)
//  3. "0.0.0-dev" (local `go run` without git info)
//
// The v prefix is stripped from BuildInfo output so "v0.1.0" → "0.1.0"
// matches the convention used by ldflags.
func Version() string {
	bi, _ := debug.ReadBuildInfo()
	return resolveVersion(version, bi)
}

// Commit returns the build's short commit SHA.
//
// Resolution order:
//  1. ldflags-injected commit (when != "dev")
//  2. debug.BuildInfo.Settings["vcs.revision"] truncated to 7 chars
//     (Go fills this for git-checkout builds via `go install` / `go run`)
//  3. "dev"
func Commit() string {
	bi, _ := debug.ReadBuildInfo()
	return resolveCommit(commit, bi)
}

// BuildDate returns the build's ISO 8601 timestamp.
//
// Resolution order:
//  1. ldflags-injected buildDate (when != "unknown")
//  2. debug.BuildInfo.Settings["vcs.time"] (commit time)
//  3. "unknown"
func BuildDate() string {
	bi, _ := debug.ReadBuildInfo()
	return resolveBuildDate(buildDate, bi)
}

// --- Pure resolvers (testable in isolation) ---

func resolveVersion(ld string, bi *debug.BuildInfo) string {
	if ld != ldSentinelVersion {
		return ld
	}
	if bi != nil && semverLike.MatchString(bi.Main.Version) {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return ldSentinelVersion
}

func resolveCommit(ld string, bi *debug.BuildInfo) string {
	if ld != ldSentinelCommit {
		return ld
	}
	if bi != nil {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				if len(s.Value) > 7 {
					return s.Value[:7]
				}
				return s.Value
			}
		}
	}
	return ldSentinelCommit
}

func resolveBuildDate(ld string, bi *debug.BuildInfo) string {
	if ld != ldSentinelBuildDate {
		return ld
	}
	if bi != nil {
		for _, s := range bi.Settings {
			if s.Key == "vcs.time" && s.Value != "" {
				return s.Value
			}
		}
	}
	return ldSentinelBuildDate
}

// --- Formatted output ---

// String returns a multi-line version string suitable for
// `awp version` output.
func String() string {
	return fmt.Sprintf(
		"awp %s\ncommit:  %s\nbuilt:   %s\ngo:      %s",
		Version(), Commit(), BuildDate(), runtime.Version(),
	)
}

// Short returns a one-line version string.
func Short() string {
	return fmt.Sprintf("awp %s (%s, %s)", Version(), Commit(), BuildDate())
}