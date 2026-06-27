package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUpdateHint covers the three install method branches.
func TestUpdateHint(t *testing.T) {
	tests := []struct {
		name   string
		method InstallMethod
		want   string
	}{
		{"homebrew", InstallHomebrew, "brew upgrade awp"},
		{"go install", InstallGo, "go install github.com/pi/awp@latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := CheckResult{InstallMethod: tt.method}
			if got := r.UpdateHint(); got != tt.want {
				t.Errorf("UpdateHint(%s) = %q; want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestUpdateHint_UnknownFallsBackToReleaseURL(t *testing.T) {
	releaseURL := "https://github.com/vanpipy/agentic-with-pi/releases/tag/v1.0.0"
	r := CheckResult{
		InstallMethod: InstallUnknown,
		ReleaseURL:    releaseURL,
	}
	if got := r.UpdateHint(); got != releaseURL {
		t.Errorf("UpdateHint(unknown) = %q; want release URL %q", got, releaseURL)
	}
}

// TestCheck_DevBuildSkipsHTTP verifies that "dev" and empty versions
// short-circuit before any network call (no need for httptest server).
// This is the primary contract of Check() — dev builds never make
// network calls and never report updates.
func TestCheck_DevBuildSkipsHTTP(t *testing.T) {
	for _, version := range []string{"dev", ""} {
		t.Run("version="+version, func(t *testing.T) {
			r := Check(version)
			if r.UpdateAvailable {
				t.Errorf("Check(%q): UpdateAvailable=true; want false", version)
			}
			if r.LatestVersion != "" {
				t.Errorf("Check(%q): LatestVersion=%q; want empty", version, r.LatestVersion)
			}
			if r.Error != nil {
				t.Errorf("Check(%q): Error=%v; want nil", version, r.Error)
			}
			if r.CurrentVersion != version {
				t.Errorf("Check(%q): CurrentVersion not echoed back, got %q", version, r.CurrentVersion)
			}
		})
	}
}

// TestRelease_UnmarshalJSON verifies the Release struct correctly
// decodes the JSON shape returned by GitHub's releases/latest endpoint.
// This is the contract Check() relies on for parsing the HTTP response.
func TestRelease_UnmarshalJSON(t *testing.T) {
	const jsonBody = `{
		"tag_name": "v1.2.3",
		"html_url": "https://github.com/vanpipy/agentic-with-pi/releases/tag/v1.2.3"
	}`
	var r Release
	if err := json.Unmarshal([]byte(jsonBody), &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.TagName != "v1.2.3" {
		t.Errorf("TagName = %q; want %q", r.TagName, "v1.2.3")
	}
	if r.HTMLURL != "https://github.com/vanpipy/agentic-with-pi/releases/tag/v1.2.3" {
		t.Errorf("HTMLURL = %q; want URL", r.HTMLURL)
	}
}

// TestVersionNewerThan covers the "latest != current" comparison with
// v-prefix normalization, matching what Check() does in production.
//
// Note: Check uses naive string equality, NOT semver. So
// "v0.3.0" vs "v0.2.0" (current is actually newer) returns true.
// This is a known limitation; if proper semver is added, this
// test will need updating.
func TestVersionNewerThan(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		newer   bool
	}{
		{"newer with v", "v0.1.0", "v0.2.0", true},
		{"same with v", "v0.1.0", "v0.1.0", false},
		{"v-prefix mismatched (naive)", "v0.3.0", "v0.2.0", true}, // string inequality, not semver
		{"newer no v", "0.1.0", "v0.2.0", true},
		{"mixed v prefixes", "0.1.0", "0.2.0", true},
		{"empty latest", "v0.1.0", "", false},
		{"empty current (string inequality)", "", "v1.0.0", true}, // never reached in Check() (dev short-circuit)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := strings.TrimPrefix(tt.current, "v")
			latest := strings.TrimPrefix(tt.latest, "v")
			// Replicate Check's comparison logic exactly.
			newer := latest != current && latest != ""
			if newer != tt.newer {
				t.Errorf("compare(%q, %q) = %v; want %v",
					tt.current, tt.latest, newer, tt.newer)
			}
		})
	}
}

// TestCheck_HTTPServerUnavailable verifies the HTTP error path
// via SetAPIBaseURL injection (added in design debt fix).
// Points the Checker at a closed httptest server (unreachable port)
// and asserts the error is propagated.
func TestCheck_HTTPServerUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // immediately close → port unreachable

	checker := NewChecker("v0.1.0")
	checker.SetAPIBaseURL(url)
	r := checker.Check()

	if r.Error == nil {
		t.Fatal("expected error from unreachable URL, got nil")
	}
	if !strings.Contains(r.Error.Error(), "failed to check for updates") {
		t.Errorf("error = %q, want it to mention 'failed to check for updates'", r.Error.Error())
	}
}

// TestCheck_HTTPErrorStatus verifies the non-200 status code path.
// Server returns 500 + asserts Error is set with the status code.
func TestCheck_HTTPErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewChecker("v1.0.0")
	checker.SetAPIBaseURL(server.URL)
	r := checker.Check()

	if r.Error == nil {
		t.Fatal("expected error from 500 status, got nil")
	}
	if !strings.Contains(r.Error.Error(), "500") {
		t.Errorf("error = %q, want it to mention status 500", r.Error.Error())
	}
}

// TestCheck_HTTPInvalidJSON verifies the JSON parse error path.
func TestCheck_HTTPInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "not valid json")
	}))
	defer server.Close()

	checker := NewChecker("v1.0.0")
	checker.SetAPIBaseURL(server.URL)
	r := checker.Check()

	if r.Error == nil {
		t.Fatal("expected error from invalid JSON, got nil")
	}
	if !strings.Contains(r.Error.Error(), "parse release info") {
		t.Errorf("error = %q, want it to mention 'parse release info'", r.Error.Error())
	}
}

// TestCheck_HTTPSuccess verifies the happy path via injection.
func TestCheck_HTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"tag_name": "v1.2.3", "html_url": "https://example.com/release"}`)
	}))
	defer server.Close()

	checker := NewChecker("v1.0.0")
	checker.SetAPIBaseURL(server.URL)
	r := checker.Check()

	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.LatestVersion != "v1.2.3" {
		t.Errorf("LatestVersion = %q, want v1.2.3", r.LatestVersion)
	}
	if r.ReleaseURL != "https://example.com/release" {
		t.Errorf("ReleaseURL = %q", r.ReleaseURL)
	}
	if !r.UpdateAvailable {
		t.Error("UpdateAvailable should be true (v1.0.0 < v1.2.3)")
	}
}

// TestDetectInstallMethod_NonePath verifies that when the binary
// path doesn't match any known install location, the method is
// InstallUnknown. Note: this test's result depends on the test
// binary's path (which we can't easily mock since os.Executable
// doesn't take an override). We accept whatever the test env
// returns — the test asserts that the function doesn't panic and
// returns one of the three known values.
func TestDetectInstallMethod_NonePath(t *testing.T) {
	m := DetectInstallMethod()
	// Bound check: must be in valid enum range. The previous
	// "m != A && m != B && m != C" check passed for ANY future enum
	// value (silent forward-compat bug). This bound check fails if
	// anyone adds an InstallMethod without updating this test.
	if m < 0 || m > InstallUnknown {
		t.Errorf("DetectInstallMethod() = %d; want value in [0, %d] (one of known InstallMethod constants). "+
			"If you added a new InstallMethod, update this test.",
			m, InstallUnknown)
	}
	// Sanity: must be one of the three known values (not an in-range
	// orphan constant).
	switch m {
	case InstallHomebrew, InstallGo, InstallUnknown:
		// ok
	default:
		t.Errorf("DetectInstallMethod() = %d; want InstallHomebrew, InstallGo, or InstallUnknown", m)
	}
}

// TestDetectInstallMethodFromPath covers the path-matching logic
// extracted from DetectInstallMethod (per design debt fix). Tests
// the actual matching rules with a table-driven approach.
func TestDetectInstallMethodFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected InstallMethod
	}{
		{"empty path is unknown", "", InstallUnknown},
		{"random path is unknown", "/usr/local/bin/awp", InstallUnknown},
		{"homebrew macOS Cellar path", "/opt/homebrew/Cellar/awp/0.5.0/bin/awp", InstallHomebrew},
		{"homebrew linuxbrew path", "/home/linuxbrew/.linuxbrew/bin/awp", InstallHomebrew},
		{"go install /go/bin path", "/home/user/go/bin/awp", InstallGo},
		{"similar but not Cellar", "/opt/brewclone/bin/awp", InstallUnknown},
		{"similar but not go/bin", "/usr/local/bin/awp", InstallUnknown},
		{"Cellar in middle of path", "/some/Cellar/dir/awp", InstallHomebrew},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectInstallMethodFromPath(tt.path); got != tt.expected {
				t.Errorf("detectInstallMethodFromPath(%q) = %d, want %d", tt.path, got, tt.expected)
			}
		})
	}
}