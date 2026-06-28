package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

// --- Pure resolver tests (independent of ldflags var state) ---

// TestResolveVersion pins the fallback chain for Version:
//
//  1. ldflags-injected value (when != "0.0.0-dev")
//  2. debug.BuildInfo.Main.Version (when semver-like, e.g. "v0.1.0")
//  3. "0.0.0-dev" sentinel
//
// Bug B fix: a `go install pkg@vX.Y.Z` binary has no ldflags but
// BuildInfo.Main.Version carries the tag, so version resolves to the
// real release version rather than "0.0.0-dev".
func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name string
		ld   string
		bi   *debug.BuildInfo
		want string
	}{
		{
			name: "ldflags wins over buildinfo",
			ld:   "0.2.0",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			want: "0.2.0",
		},
		{
			name: "buildinfo semver when ldflags at default",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			want: "0.1.0", // v prefix stripped
		},
		{
			name: "buildinfo semver without v prefix",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "1.2.3"}},
			want: "1.2.3",
		},
		{
			name: "buildinfo devel falls through to default",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: "0.0.0-dev",
		},
		{
			name: "buildinfo empty falls through to default",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: ""}},
			want: "0.0.0-dev",
		},
		{
			name: "nil buildinfo falls through to default",
			ld:   "0.0.0-dev",
			bi:   nil,
			want: "0.0.0-dev",
		},
		{
			name: "buildinfo prerelease semver",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0-rc.1"}},
			want: "1.0.0-rc.1",
		},
		{
			// Go's pseudo-version (e.g. from `make build` without
			// ldflags, or `go install` on a non-tagged commit) has
			// the form X.Y.Z-0.YYYYMMDDHHMMSS-<sha>. A dev build
			// should not be reported as a real version — it leaks
			// Go module internals and looks like a release tag.
			// The resolver must fall through to the dev sentinel.
			name: "go pseudo-version falls through to default",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "0.1.1-0.20260628084246-c82a5e55c1b9"}},
			want: "0.0.0-dev",
		},
		{
			name: "go pseudo-version with v prefix falls through to default",
			ld:   "0.0.0-dev",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v0.1.1-0.20260628084246-c82a5e55c1b9"}},
			want: "0.0.0-dev",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.ld, tc.bi); got != tc.want {
				t.Errorf("resolveVersion(%q, %+v) = %q, want %q", tc.ld, tc.bi, got, tc.want)
			}
		})
	}
}

// TestResolveCommit pins the fallback chain for Commit:
//
//  1. ldflags-injected value (when != "dev")
//  2. debug.BuildInfo.Settings["vcs.revision"] (full SHA, truncated to 7)
//  3. "dev"
func TestResolveCommit(t *testing.T) {
	cases := []struct {
		name string
		ld   string
		bi   *debug.BuildInfo
		want string
	}{
		{
			name: "ldflags wins over vcs",
			ld:   "abc1234",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "deadbeef1234567"}}},
			want: "abc1234",
		},
		{
			name: "vcs.revision truncated to 7 chars",
			ld:   "dev",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc1234567890abcdef"}}},
			want: "abc1234",
		},
		{
			name: "short vcs.revision preserved as-is",
			ld:   "dev",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}},
			want: "abc",
		},
		{
			name: "no vcs.revision falls through to dev",
			ld:   "dev",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"}}},
			want: "dev",
		},
		{
			name: "nil buildinfo falls through to dev",
			ld:   "dev",
			bi:   nil,
			want: "dev",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveCommit(tc.ld, tc.bi); got != tc.want {
				t.Errorf("resolveCommit(%q, %+v) = %q, want %q", tc.ld, tc.bi, got, tc.want)
			}
		})
	}
}

// TestResolveBuildDate pins the fallback chain for BuildDate:
//
//  1. ldflags-injected value (when != "unknown")
//  2. debug.BuildInfo.Settings["vcs.time"] (commit time, RFC3339-ish)
//  3. "unknown"
func TestResolveBuildDate(t *testing.T) {
	cases := []struct {
		name string
		ld   string
		bi   *debug.BuildInfo
		want string
	}{
		{
			name: "ldflags wins over vcs.time",
			ld:   "2026-06-28T14:22:52Z",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.time", Value: "2025-01-01T00:00:00Z"}}},
			want: "2026-06-28T14:22:52Z",
		},
		{
			name: "vcs.time used when ldflags at default",
			ld:   "unknown",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.time", Value: "2026-01-15T12:34:56Z"}}},
			want: "2026-01-15T12:34:56Z",
		},
		{
			name: "no vcs.time falls through to unknown",
			ld:   "unknown",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc1234"}}},
			want: "unknown",
		},
		{
			name: "nil buildinfo falls through to unknown",
			ld:   "unknown",
			bi:   nil,
			want: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBuildDate(tc.ld, tc.bi); got != tc.want {
				t.Errorf("resolveBuildDate(%q, %+v) = %q, want %q", tc.ld, tc.bi, got, tc.want)
			}
		})
	}
}

// --- Integration: Version()/Commit()/BuildDate() in the actual binary ---

// TestVersion_NoPanic ensures the public function form doesn't panic
// when called from a normal binary (the BuildInfo read may fail, but
// must return a sane fallback).
func TestVersion_NoPanic(t *testing.T) {
	v := Version()
	if v == "" {
		t.Error("Version() should never return empty string")
	}
}

func TestCommit_NoPanic(t *testing.T) {
	c := Commit()
	if c == "" {
		t.Error("Commit() should never return empty string")
	}
}

func TestBuildDate_NoPanic(t *testing.T) {
	bd := BuildDate()
	if bd == "" {
		t.Error("BuildDate() should never return empty string")
	}
}

// --- String / Short format regression ---

func TestString(t *testing.T) {
	s := String()
	for _, want := range []string{"awp", "commit:", "built:", "go:"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q. Got: %s", want, s)
		}
	}
}

func TestShort(t *testing.T) {
	s := Short()
	if strings.Count(s, "\n") != 0 {
		t.Errorf("Short() should be one line, got: %s", s)
	}
	if !strings.HasPrefix(s, "awp ") {
		t.Errorf("Short() should start with 'awp ', got: %s", s)
	}
}

// TestDefaults pins that the ldflags-target vars (now lowercase) keep
// sensible default values, so a binary built without -ldflags still
// reports non-empty version info.
func TestDefaults(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
	if buildDate == "" {
		t.Error("buildDate should not be empty")
	}
}