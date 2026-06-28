//go:build integration

package terminal_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSpawn_TwiceConsecutively reproduces the user-reported bug:
// "第一次 spawn 闪退, 第二次 spawn 界面停滞"
//
// Two Pane.Start() calls back-to-back, both with mock-pi.sh.
// First should produce OutputMsg (ModeSpawning → ModeAgentView).
// Second should ALSO produce OutputMsg — the user reports the
// second one stalls.
func TestSpawn_TwiceConsecutively(t *testing.T) {
	testutil.RequireLinux(t)
	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh")

	// === First spawn ===
	pane1 := terminal.New("ticket-1", 80, 24, 0)
	pane1.SetWorkdir(t.TempDir())

	cmd1 := pane1.StartCmd(mockPath, "--mode", "rpc")
	if cmd1 == nil {
		t.Fatal("first: Start returned nil")
	}

	// Drive the self-cycle: each readOutput returns OutputMsg.
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		msg := cmd1()
		if msg == nil {
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			next := pane1.Update(outMsg)
			if next == nil {
				break
			}
			cmd1 = next
		} else if _, ok := msg.(terminal.ExitMsg); ok {
			break
		}
	}

	if !pane1.Running() {
		t.Errorf("first: pane.Running() = false; want true")
	}

	content1 := pane1.GetContent()
	if !strings.Contains(content1, "agent_start") {
		t.Errorf("first: missing agent_start in content; got: %q",
			content1[:min(200, len(content1))])
	}

	// === Second spawn — the critical case ===
	pane2 := terminal.New("ticket-2", 80, 24, 0)
	pane2.SetWorkdir(t.TempDir())

	cmd2 := pane2.StartCmd(mockPath, "--mode", "rpc")
	if cmd2 == nil {
		t.Fatal("second: Start returned nil")
	}

	outputReceived := false
	for i := 0; i < 30; i++ {
		time.Sleep(50 * time.Millisecond)
		msg := cmd2()
		if msg == nil {
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			outputReceived = true
			next := pane2.Update(outMsg)
			if next == nil {
				break
			}
			cmd2 = next
		} else if _, ok := msg.(terminal.ExitMsg); ok {
			break
		}
	}

	if !outputReceived {
		t.Error("second spawn: never received OutputMsg — UI would stall on ModeSpawning")
	}

	if !pane2.Running() {
		t.Error("second spawn: pane.Running() = false; want true")
	}

	content2 := pane2.GetContent()
	if !strings.Contains(content2, "agent_start") {
		t.Errorf("second: missing agent_start in content; got: %q",
			content2[:min(200, len(content2))])
	}

	// Cleanup
	pane1.Stop()
	pane2.Stop()
}
