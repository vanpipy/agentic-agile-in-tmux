//go:build integration

// cycle_integration_test.go — end-to-end smoke for the postman.
//
// This is the MVG (Minimum Viable Goal) test per the sprint:
// one full wiking → coding → accept round against a fake pi
// binary in a temp git repo, verifying that the cycle:
//
//   1. Spawns the wiking subprocess with --role wiking.
//   2. Polls the marker file and detects --- end ---.
//   3. Spawns the coding subprocess with --role coding.
//   4. Polls the marker file and detects --- end with N ---.
//   5. Syncs the canonical article.md file.
//   6. Emits cycle_accepted, exits 0.
//
// Requires Linux (fake script is /bin/sh). Run with:
//
//   go test -tags=integration ./test/wiking/

package wiking_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pi/awp/internal/testutil"
	"github.com/pi/awp/internal/wiking"
)

// fakePiScript is a fake pi binary. Inspects --role and writes a
// markdown file with the appropriate marker. Real pi is far more
// capable, but for the MVG we only need marker writing.
//
// Note: arg parsing uses `while [ $# -gt 0 ]` with `shift`, not a
// `for arg in "$@"` loop. `shift` inside a `for` loop does NOT
// re-evaluate the loop's expansion (the captured list is fixed at
// loop start), which led to a 22-hour test hang until I caught it.
const fakePiScript = `#!/bin/sh
role=""
while [ $# -gt 0 ]; do
    case "$1" in
        --role) role="$2"; shift 2 ;;
        *) shift ;;
    esac
done

case "$role" in
    wiking)
        # Write article-1.md with the wiking-end marker.
        cat > article-1.md <<'BODY'
# Wiking Draft (round 1)

This is the wiking role's draft discussing the postman protocol.
BODY
        printf '\n--- end ---\n' >> article-1.md
        exit 0
        ;;
    coding)
        # Write article-1-feedback-1.md with the score marker.
        cat > article-1-feedback-1.md <<'BODY'
# Coding Review (round 1)

The draft is well-structured and complete.
BODY
        printf '\n--- end with 92 ---\n' >> article-1-feedback-1.md
        exit 0
        ;;
esac

# Unknown role: fail loudly so the test surfaces the bug.
echo "fake-pi: unknown role: $role" >&2
exit 99
`

// TestCycle_OneFullRound drives one full wiking → coding → accept
// round against a fake pi binary.
func TestCycle_OneFullRound(t *testing.T) {
	testutil.RequireLinux(t)

	// Temp wiki (and a git init so we look like a real repo).
	wiki := t.TempDir()
	runShell(t, wiki, "git", "init", "-q")
	runShell(t, wiki, "git", "config", "user.email", "test@example.com")
	runShell(t, wiki, "git", "config", "user.name", "Test")

	// AWP home — where events.jsonl lives.
	awp := t.TempDir()

	// Fake pi binary.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fake-pi")
	if err := os.WriteFile(binPath, []byte(fakePiScript), 0o755); err != nil {
		t.Fatalf("write fake pi: %v", err)
	}

	cfg := wiking.Config{
		WikiDir:        wiki,
		RunID:          "round1",
		AWPHome:        awp,
		Threshold:      90,
		IdleInterval:   100 * time.Millisecond,
		WikingInterval: 100 * time.Millisecond,
		CodingInterval: 100 * time.Millisecond,
		WikingTimeout:  10 * time.Second,
		CodingTimeout:  10 * time.Second,
		MaxNoProgress:  50,
		Wiking:         wiking.RoleBinding{Prompt: "test-wiking", CWD: wiki},
		Coding:         wiking.RoleBinding{Prompt: "test-coding", CWD: wiki},
		Binary:         binPath,
	}

	cyc, err := wiking.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- cyc.Run(ctx) }()

	// Drain events into a list while Run is active; exit when Run
	// returns. (Events channel isn't closed by the cycle; we exit
	// the drain goroutine via the runDone signal.)
	collected := make(chan []wiking.Event, 1)
	go func() {
		var evs []wiking.Event
		for {
			select {
			case ev := <-cyc.Events:
				evs = append(evs, ev)
			case <-runDone:
				collected <- evs
				return
			}
		}
	}()

	select {
	case err := <-runDone:
		if err != nil {
			evs := <-collected
			t.Logf("Run err: %v — events (%d):", err, len(evs))
			for _, e := range evs {
				t.Logf("  %s round=%v", e.Type, e.Round)
			}
			t.Fatalf("Run err: %v", err)
		}
	case <-time.After(28 * time.Second):
		// Take a peek into events even on timeout so we can debug.
		select {
		case evs := <-collected:
			t.Logf("timeout: events collected (%d):", len(evs))
			for _, e := range evs {
				t.Logf("  %s round=%v", e.Type, e.Round)
			}
		case <-time.After(100 * time.Millisecond):
		}
		t.Fatal("Run did not return within 28s")
	}

	evs := <-collected

	// Verify the full event sequence is plausible.
	types := eventTypes(evs)
	wantTypes := []string{
		"round_started", "wiking_spawned",
		"wiking_done", "coding_spawned",
		"coding_done", "score_parsed",
		"score_above_threshold", "synced",
		"cycle_accepted",
	}
	if !containsAll(types, wantTypes) {
		t.Fatalf("missing event types: got %v want all of %v", types, wantTypes)
	}

	// Verify canonical article.md exists with wiking content.
	canonPath := filepath.Join(wiki, "article.md")
	canonical, err := os.ReadFile(canonPath)
	if err != nil {
		t.Fatalf("canonical missing: %v", err)
	}
	if !strings.Contains(string(canonical), "--- end ---") {
		t.Errorf("canonical missing wiking-end marker: %q", canonical)
	}
	if !strings.Contains(string(canonical), "Wiking Draft") {
		t.Errorf("canonical missing expected content: %q", canonical)
	}

	// Verify the wiking artifact exists with valid marker.
	articlePath := filepath.Join(wiki, "article-1.md")
	body, err := os.ReadFile(articlePath)
	if err != nil {
		t.Fatalf("article-1.md missing: %v", err)
	}
	ok, _ := wiking.CheckEnd(articlePath)
	if !ok {
		t.Errorf("CheckEnd(article-1.md) = false; want true. Body: %q", body)
	}

	// Verify the feedback artifact with high score.
	fbPath := filepath.Join(wiki, "article-1-feedback-1.md")
	score, err := wiking.CheckScore(fbPath)
	if err != nil {
		t.Fatalf("CheckScore error: %v", err)
	}
	if score < 90 {
		t.Errorf("score = %d, want >= 90", score)
	}

	// Verify the events.jsonl audit log has the same events.
	eventsPath := filepath.Join(awp, "cycle", "round1", "events.jsonl")
	logged, err := wiking.ReadLines(eventsPath, "")
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	loggedTypes := eventTypes(logged)
	if !containsAll(loggedTypes, wantTypes) {
		t.Fatalf("events.jsonl missing types: got %v want all of %v",
			loggedTypes, wantTypes)
	}
}

// eventTypes projects a slice of events to their Type strings.
func eventTypes(evs []wiking.Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// containsAll reports whether every element of need is in got (order-agnostic).
func containsAll(got, need []string) bool {
	set := make(map[string]int, len(got))
	for _, t := range got {
		set[t]++
	}
	for _, t := range need {
		if set[t] == 0 {
			return false
		}
		set[t]--
	}
	return true
}

func runShell(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shell: %s %v: %v\n%s", name, args, err, out)
	}
}

// Sanity check: ensure the cycle library works without integration
// (i.e., the tests file builds even with the build tag).
var _ = json.Marshal
