// dispatch_test.go — RED tests for the subprocess spawn layer.
//
// Per SYSTEM_DESIGN.md §18.3 / §18.11: Dispatch is the file in
// charge of starting the pi subprocess under the role binding.
// It does not interpret pi semantics — that's the cycle driver.
// Tests use fake binaries (shell scripts written to t.TempDir)
// so they're hermetic and not coupled to a real pi install.

package wiking

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpawn_HappyPath(t *testing.T) {
	bin := writeFakeBin(t, "ok.sh", `echo hello
exit 0
`)
	d := NewDispatch().WithBinary(bin)
	cmd, err := d.Spawn(context.Background(), SpawnArgs{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestSpawn_EmptyBinaryErrors(t *testing.T) {
	d := NewDispatch().WithBinary("")
	_, err := d.Spawn(context.Background(), SpawnArgs{})
	if err == nil {
		t.Fatal("expected error for empty binary")
	}
}

func TestSpawn_StartMissingBinary(t *testing.T) {
	d := NewDispatch().WithBinary("/nonexistent/binary/path/never/exists")
	cmd, err := d.Spawn(context.Background(), SpawnArgs{})
	if err != nil {
		t.Fatalf("Spawn (no validation) returned %v", err)
	}
	if err := cmd.Start(); err == nil {
		t.Fatal("expected error from Start() with missing binary")
	}
}

func TestSpawn_DirHonored(t *testing.T) {
	tmp := t.TempDir()
	bin := writeFakeBin(t, "pwd.sh", `pwd
exit 0
`)
	var buf bytes.Buffer
	d := NewDispatch().WithBinary(bin).WithStdout(&buf)
	cmd, err := d.Spawn(context.Background(), SpawnArgs{Dir: tmp})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != tmp {
		t.Fatalf("pwd got %q want %q", got, tmp)
	}
}

func TestSpawn_StdinPayloadDelivered(t *testing.T) {
	bin := writeFakeBin(t, "echo_stdin.sh", `cat
exit 0
`)
	var buf bytes.Buffer
	d := NewDispatch().WithBinary(bin).WithStdout(&buf)
	cmd, err := d.Spawn(context.Background(), SpawnArgs{
		StdinPayload: "hello-from-test\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "hello-from-test") {
		t.Fatalf("stdin not delivered: %q", buf.String())
	}
}

func TestSpawn_ArgsForwardedToSubprocess(t *testing.T) {
	bin := writeFakeBin(t, "print_args.sh", `
for a in "$@"; do
    echo "ARG: $a"
done
exit 0
`)
	var buf bytes.Buffer
	d := NewDispatch().WithBinary(bin).WithStdout(&buf)
	cmd, err := d.Spawn(context.Background(), SpawnArgs{
		Args: []string{"--mode", "rpc", "--extension", "/tmp/ext.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"ARG: --mode", "ARG: rpc", "ARG: --extension", "ARG: /tmp/ext.ts"}
	for _, w := range want {
		if !strings.Contains(buf.String(), w) {
			t.Fatalf("subprocess did not see arg %s; got: %q", w, buf.String())
		}
	}
}

func TestSpawn_DiscardStdoutByDefault(t *testing.T) {
	bin := writeFakeBin(t, "loud.sh", `echo VERY-LOUD-AND-DISRUPTIVE
exit 0
`)
	d := NewDispatch()
	cmd, err := d.Spawn(context.Background(), SpawnArgs{Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Stdout != io.Discard {
		t.Fatalf("expected cmd.Stdout == io.Discard, got %T", cmd.Stdout)
	}
}

func TestSpawn_DiscardStderrByDefault(t *testing.T) {
	bin := writeFakeBin(t, "noisy.sh", `echo NOISY >&2
exit 0
`)
	d := NewDispatch()
	cmd, err := d.Spawn(context.Background(), SpawnArgs{Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Stderr != io.Discard {
		t.Fatalf("expected cmd.Stderr == io.Discard, got %T", cmd.Stderr)
	}
}

func TestSpawn_ContextCancelTerminatesProcess(t *testing.T) {
	// A 30s sleep; cancel ctx after 200ms; expect Wait to return
	// well before the 30s would have elapsed.
	bin := writeFakeBin(t, "long.sh", `sleep 30
exit 0
`)
	d := NewDispatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd, err := d.Spawn(ctx, SpawnArgs{Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected non-nil error from Wait after cancel")
		}
		if !isAcceptableTermination(err) {
			t.Fatalf("Wait err not a signal termination: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return within 10s after cancel — process leaked")
		cmd.Process.Kill()
	}
}

func TestSpawn_CancelSendsSIGTERMBeforeSIGKILL(t *testing.T) {
	// Cmd.Cancel is set: ctx cancellation sends SIGTERM first;
	// after WaitDelay, escalates to SIGKILL.
	bin := writeFakeBin(t, "ignore_term.sh", `
trap '' TERM
sleep 30
exit 0
`)
	d := NewDispatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	cmd, err := d.Spawn(ctx, SpawnArgs{Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = cmd.Wait()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected non-nil err from Wait after cancel")
	}
	// SIGTERM is ignored, so the cmd's WaitDelay timeout should kick
	// in (5s by default). Allow generous slack for slow CI.
	if elapsed > 8*time.Second {
		t.Fatalf("Wait took %v, expected <=8s (SIGTERM->WaitDelay->SIGKILL)", elapsed)
	}
}

// isAcceptableTermination returns true for errors that indicate
// the process died from a signal — which is what we expect when
// ctx cancels a subprocess.
func isAcceptableTermination(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return !ee.Success()
	}
	return true
}

// Helpers below — fake-binary script writer.

func writeFakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
	return path
}
