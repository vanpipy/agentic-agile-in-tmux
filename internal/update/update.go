package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	apiTimeout = 5 * time.Second
)

// githubRepo is the \"owner/repo\" used to query the GitHub releases API.
// It's a var (not const) so tests can override it via setGithubRepo().
// The default is set at package init time.
var githubRepo = "vanpipy/agentic-with-pi"

type InstallMethod int

const (
	InstallUnknown InstallMethod = iota
	InstallHomebrew
	InstallGo
)

// Release represents a GitHub release
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckResult contains the result of an update check
type CheckResult struct {
	UpdateAvailable bool
	LatestVersion   string
	CurrentVersion  string
	ReleaseURL      string
	InstallMethod   InstallMethod
	Error           error
}

// UpdateHint returns a user-friendly update command based on install method
func (r CheckResult) UpdateHint() string {
	switch r.InstallMethod {
	case InstallHomebrew:
		return "brew upgrade awp"
	case InstallGo:
		return "go install github.com/" + githubRepo + "@latest"
	default:
		return r.ReleaseURL
	}
}

// DetectInstallMethod determines how awp was installed
func DetectInstallMethod() InstallMethod {
	exe, err := os.Executable()
	if err != nil {
		return InstallUnknown
	}
	return detectInstallMethodFromPath(exe)
}

// detectInstallMethodFromPath matches the install method based on
// the executable path. Extracted from DetectInstallMethod so tests
// can exercise the path-matching logic without depending on the
// test binary's location (which os.Executable doesn't allow mocking).
func detectInstallMethodFromPath(exePath string) InstallMethod {
	if strings.Contains(exePath, "Cellar") || strings.Contains(exePath, "linuxbrew") {
		return InstallHomebrew
	}
	if strings.Contains(exePath, "/go/bin") {
		return InstallGo
	}
	return InstallUnknown
}

// Checker provides update checking functionality
type Checker struct {
	CurrentVersion string
	apiBaseURL     string // override for testing; empty = use GitHub
}

// NewChecker creates a new update checker for the given version
func NewChecker(currentVersion string) *Checker {
	return &Checker{CurrentVersion: currentVersion}
}

// SetAPIBaseURL overrides the API endpoint (testing only).
// Empty string restores the default GitHub endpoint.
func (c *Checker) SetAPIBaseURL(url string) {
	c.apiBaseURL = url
}

// Check compares the current version against the latest GitHub release.
// Returns immediately if current version is "dev" (development build).
func (c *Checker) Check() CheckResult {
	result := CheckResult{
		CurrentVersion: c.CurrentVersion,
	}

	if c.CurrentVersion == "dev" || c.CurrentVersion == "" {
		return result
	}

	client := &http.Client{Timeout: apiTimeout}
	url := c.apiBaseURL
	if url == "" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	}

	// GitHub API requires a User-Agent header. Unidentified requests are
	// rate-limited more aggressively. See:
	//   https://docs.github.com/en/rest/overview/resources-in-the-rest-api#user-agent-required
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to build update request: %w", err)
		return result
	}
	req.Header.Set("User-Agent", "awp/"+c.CurrentVersion+" (+https://github.com/"+githubRepo+")")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to check for updates: %w", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		return result
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		result.Error = fmt.Errorf("failed to parse release info: %w", err)
		return result
	}

	result.LatestVersion = release.TagName
	result.ReleaseURL = release.HTMLURL
	result.InstallMethod = DetectInstallMethod()

	current := strings.TrimPrefix(c.CurrentVersion, "v")
	latest := strings.TrimPrefix(release.TagName, "v")

	if latest != current && latest != "" {
		result.UpdateAvailable = true
	}

	return result
}

// Check is a package-level helper that creates a default Checker.
// Use NewChecker + SetAPIBaseURL if you need to inject the API URL.
func Check(currentVersion string) CheckResult {
	return NewChecker(currentVersion).Check()
}
