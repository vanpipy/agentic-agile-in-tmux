package observability

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSmoke_SampleOutput is a developer-facing sanity check that
// produces a real log line and crash file so a human can inspect
// the format. Skipped by default; run with -run TestSmoke.
func TestSmoke_SampleOutput(t *testing.T) {
	if os.Getenv("AWP_SMOKE") == "" {
		t.Skip("set AWP_SMOKE=1 to run smoke output")
	}
	dir := filepath.Join(os.TempDir(), "awp-smoke")
	_ = os.RemoveAll(dir)
	if err := Init(false, dir); err != nil {
		t.Fatal(err)
	}
	Info("smoke info — should NOT appear in file at default level")
	Warn("smoke warn", "ticket", "abc-123", "phase", "spawn")
	Error("smoke error", "err", "synthetic")
	time.Sleep(50 * time.Millisecond)

	today := filepath.Join(dir, "awp-"+time.Now().Format("2006-01-02")+".log")
	fmt.Fprintf(os.Stderr, "\n=== daily log at %s ===\n", today)
	data, _ := os.ReadFile(today)
	os.Stderr.Write(data)

	crashPath, err := WriteCrashFile(dir, "synthetic-panic", []byte("goroutine 1 [running]:\nmain.foo()\n\tmain.go:42 +0x27\n"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "\n=== crash file at %s ===\n", crashPath)
	cdata, _ := os.ReadFile(crashPath)
	os.Stderr.Write(cdata)

	// Make the assertion fail so the test output is visible
	t.Fatal(strings.Repeat("=", 60) + " see above")
}
