package awp

import (
	"strings"
	"testing"
)

// readRootGoSource is defined in root_projectcmd_test.go (walks up
// from CWD to go.mod). We reuse it to keep the source-reading logic
// consistent across test files in this package.

// TestRootCmd_NoStaleVersionConstant guards against the
// audit_2026_06_27 NOSE-1 regression: a `const version = "0.0.0-dev"`
// in root.go that diverged from buildinfo.Version. With that const
// present, `awp --version` and observability.Debug logs could disagree
// from ldflags-injected values.
//
// We assert at the source level because at runtime a const-bound
// rootCmd.Version and a default buildinfo.Version="0.0.0-dev" can
// be string-equal in dev builds, hiding the divergence.
func TestRootCmd_NoStaleVersionConstant(t *testing.T) {
	src := readRootGoSource(t)
	const forbidden = `const version = "`
	if strings.Contains(src, forbidden) {
		t.Errorf("root.go contains forbidden declaration %s — versions must come from internal/buildinfo only", forbidden)
	}
}

// TestRootCmd_ReferencesBuildinfoVersion asserts root.go references
// buildinfo.Version in the two places that previously used the local
// `version` const:
//
//	(1) rootCmd.Version = buildinfo.Version      // cobra --version
//	(2) observability.Debug(... buildinfo.Version) // structured log
//
// Both call sites must agree. A regression that drops one reference
// (e.g. only updates rootCmd.Version and leaves the log call using
// a now-removed const) fails this test.
func TestRootCmd_ReferencesBuildinfoVersion(t *testing.T) {
	src := readRootGoSource(t)
	const want = 2
	if got := strings.Count(src, "buildinfo.Version"); got != want {
		t.Errorf("root.go should reference buildinfo.Version %d times (rootCmd.Version + observability.Debug), got %d", want, got)
	}
}
