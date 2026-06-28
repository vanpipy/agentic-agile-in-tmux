// Command releasecheck is the pre-flight validator used by `make release`.
//
// Usage:
//
//	go run ./cmd/releasecheck <version> <expected-branch>
//
// Exit codes:
//
//	0 — all checks passed; print "releasecheck OK: ..."
//	1 — a check failed (semver / git / tag); reason on stderr
//	2 — invocation error (missing args)
//
// `make release` calls this with `go run ./cmd/releasecheck $(VERSION) main`.
// Splitting the pre-flight into its own binary keeps the Makefile declarative
// and lets the same checks be reused by a future CI workflow without
// re-implementing them.
package main

import (
	"fmt"
	"os"

	"github.com/pi/awp/internal/release"
)

// runChecks is the testable entry point. It returns (message, exitCode).
// On success, message is human-readable status. On failure, message is empty
// (the caller should print a generic prefix and use the error).
func runChecks(args []string) (string, int, error) {
	if len(args) < 3 {
		return "", 2, fmt.Errorf("usage: releasecheck <version> <expected-branch>")
	}
	version := args[1]
	branch := args[2]

	if err := release.ValidateSemver(version); err != nil {
		return "", 1, fmt.Errorf("semver check failed: %w", err)
	}
	if err := release.AssertGitClean(); err != nil {
		return "", 1, fmt.Errorf("git clean check failed: %w", err)
	}
	if err := release.AssertOnBranch(branch); err != nil {
		return "", 1, fmt.Errorf("branch check failed: %w", err)
	}
	tag := release.BuildTag(version)
	if err := release.AssertTagAbsent(tag); err != nil {
		return "", 1, fmt.Errorf("tag check failed: %w", err)
	}
	return fmt.Sprintf("releasecheck OK: version=%s tag=%s branch=%s", version, tag, branch), 0, nil
}

func main() {
	msg, code, err := runChecks(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if msg != "" && code == 0 {
		fmt.Println(msg)
	}
	os.Exit(code)
}
