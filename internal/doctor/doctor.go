// Package doctor — `awp doctor` self-diagnostics (Phase 5).
//
// Runs a series of checks on the user's environment and reports
// which pass/fail. Used to debug "why isn't awp working?" issues.
//
// Checks:
//  1. pi binary on PATH
//  2. pi --version works
//  3. ~/.config/awp dir exists (created if --fix)
//  4. ~/.pi/agent dir exists
//  5. git available
//  6. current dir is a git repo
//  7. has registered projects
//
// Each Check returns (name, passed, message). The runner formats
// results as a table.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// Check is a single diagnostic check.
type Check struct {
	Name    string
	Passed  bool
	Message string
}

// Result aggregates all checks + summary.
type Result struct {
	Checks []Check
	AllOK  bool
}

// Runner holds the state for a doctor run.
type Runner struct {
	// CWD is the working directory for the checks. Defaults to
	// os.Getwd() if empty.
	CWD string
	// Fix enables auto-fix for fixable issues (e.g. create config dir).
	Fix bool
}

// NewRunner creates a Runner with defaults.
func NewRunner() *Runner {
	return &Runner{}
}

// Run executes all checks and returns the aggregated result.
func (r *Runner) Run() (*Result, error) {
	if r.CWD == "" {
		cwd, _ := os.Getwd()
		r.CWD = cwd
	}
	checks := []Check{
		r.checkPiBinary(),
		r.checkPiVersion(),
		r.checkAwpConfigDir(),
		r.checkPiAgentDir(),
		r.checkGitAvailable(),
		r.checkCwdIsGitRepo(),
		r.checkRegisteredProjects(),
	}
	allOK := true
	for _, c := range checks {
		if !c.Passed {
			allOK = false
			break
		}
	}
	return &Result{Checks: checks, AllOK: allOK}, nil
}

// checkPiBinary verifies `pi` is on PATH.
func (r *Runner) checkPiBinary() Check {
	path, err := exec.LookPath("pi")
	if err != nil {
		return Check{
			Name:    "pi binary on PATH",
			Passed:  false,
			Message: "pi not found. Install: npm i -g @earendil-works/pi-coding-agent",
		}
	}
	return Check{
		Name:    "pi binary on PATH",
		Passed:  true,
		Message: path,
	}
}

// checkPiVersion verifies `pi --version` runs successfully.
func (r *Runner) checkPiVersion() Check {
	cmd := exec.Command("pi", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Check{
			Name:    "pi --version works",
			Passed:  false,
			Message: fmt.Sprintf("error: %v (output: %s)", err, strings.TrimSpace(string(out))),
		}
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		version = "(empty output)"
	}
	return Check{
		Name:    "pi --version works",
		Passed:  true,
		Message: version,
	}
}

// checkAwpConfigDir verifies the awp config dir exists or can be created.
func (r *Runner) checkAwpConfigDir() Check {
	path, err := config.ConfigDir()
	if err != nil {
		return Check{
			Name:    "awp config dir",
			Passed:  false,
			Message: err.Error(),
		}
	}
	if _, err := os.Stat(path); err != nil {
		if r.Fix {
			if mkErr := os.MkdirAll(path, 0o755); mkErr != nil {
				return Check{
				Name:    "awp config dir",
				Passed:  false,
				Message: fmt.Sprintf("%s does not exist, mkdir failed: %v", path, mkErr),
				}
			}
			return Check{
				Name:    "awp config dir",
				Passed:  true,
				Message: path + " (created)",
			}
		}
		return Check{
			Name:    "awp config dir",
			Passed:  false,
			Message: path + " does not exist (run with --fix to create)",
		}
	}
	return Check{
		Name:    "awp config dir",
		Passed:  true,
		Message: path,
	}
}

// checkPiAgentDir verifies ~/.pi/agent exists (where pi stores sessions).
func (r *Runner) checkPiAgentDir() Check {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".pi", "agent")
	if _, err := os.Stat(dir); err != nil {
		return Check{
			Name:    "pi agent dir",
			Passed:  false,
			Message: dir + " (no pi sessions yet; this is normal for new users)",
		}
	}
	// Count session files
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			count++
		}
		return nil
	})
	return Check{
		Name:    "pi agent dir",
		Passed:  true,
		Message: fmt.Sprintf("%s (%d session files)", dir, count),
	}
}

// checkGitAvailable verifies `git` is on PATH.
func (r *Runner) checkGitAvailable() Check {
	path, err := exec.LookPath("git")
	if err != nil {
		return Check{
			Name:    "git available",
			Passed:  false,
			Message: "git not found on PATH",
		}
	}
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return Check{
			Name:    "git available",
			Passed:  false,
			Message: fmt.Sprintf("git --version failed: %v", err),
		}
	}
	version := strings.TrimSpace(string(out))
	return Check{
		Name:    "git available",
		Passed:  true,
		Message: path + " (" + version + ")",
	}
}

// checkCwdIsGitRepo verifies the current directory is inside a git repo.
func (r *Runner) checkCwdIsGitRepo() Check {
	cmd := exec.Command("git", "-C", r.CWD, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return Check{
			Name:    "cwd is git repo",
			Passed:  false,
			Message: r.CWD + " is not a git repository",
		}
	}
	return Check{
		Name:    "cwd is git repo",
		Passed:  true,
		Message: r.CWD,
	}
}

// checkRegisteredProjects verifies there are registered awp projects.
func (r *Runner) checkRegisteredProjects() Check {
	registry, err := project.LoadRegistry()
	if err != nil {
		return Check{
			Name:    "registered projects",
			Passed:  false,
			Message: err.Error(),
		}
	}
	projects := registry.List()
	if len(projects) == 0 {
		return Check{
			Name:    "registered projects",
			Passed:  false,
			Message: "no projects registered. Run: awp project new <name>",
		}
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}
	return Check{
		Name:    "registered projects",
		Passed:  true,
		Message: fmt.Sprintf("%d: %s", len(projects), strings.Join(names, ", ")),
	}
}

// Format returns a human-readable table.
func (r *Result) Format(verbose bool) string {
	var b strings.Builder
	b.WriteString("awp doctor\n")
	b.WriteString(strings.Repeat("=", 40) + "\n\n")
	for _, c := range r.Checks {
		mark := "✓"
		if !c.Passed {
			mark = "✗"
		}
		fmt.Fprintf(&b, "  %s  %s\n", mark, c.Name)
		if verbose || !c.Passed {
			fmt.Fprintf(&b, "       %s\n", c.Message)
		}
	}
	b.WriteString("\n")
	if r.AllOK {
		b.WriteString("All checks passed ✓\n")
	} else {
		b.WriteString("Some checks failed. Run with --verbose for details.\n")
	}
	return b.String()
}
