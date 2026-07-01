// notify_after_init_dies_test.go — audit regression test for the
// "tick dies after first empty-state notification" defect.
//
// Audit finding (gaoyao DEATH phase, c24e035): the case notificationMsg
// handler returns nil when m.notification == "", so the tick dies after
// the first invocation if no toast is on screen at that moment. Init()
// schedules a one-shot tickNotification(500ms). At t=500ms with no toast,
// the handler returns nil. Any subsequent m.notify() call sets the toast
// but cannot restart the tick, so the toast stays forever — the exact
// bug the fix was meant to eliminate.
//
// This test pins the regression. It SHOULD fail on c24e035 (current
// state); passing it requires the handler to re-arm the tick
// unconditionally (matching the tickAgentStatus pattern).
package ui

import (
	"testing"
	"time"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestNotify_AfterFirstTickDies_NoReArmWithoutNotification captures the
// audit-found defect: the empty-state tick dies and the next m.notify()
// has no tick to clear it.
func TestNotify_AfterFirstTickDies_NoReArmWithoutNotification(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test-proj", tmpDir)
	registry.Projects[p.ID] = p
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	m := NewModel(config.DefaultConfig(), gts, registry, "", nil)

	// Simulate t=500ms after Init(): the pre-scheduled tick fires.
	// No toast has been set, so the handler MUST re-arm the tick
	// (otherwise it dies and subsequent m.notify() calls have no
	// way to schedule a clear).
	_, cmd := m.Update(notificationMsg(time.Now()))
	if cmd == nil {
		t.Errorf("first empty-state notification tick died (cmd == nil).\n"+
			"Audit finding: handler returns nil when m.notification == \"\".\n"+
			"Subsequent m.notify() calls have no tick running, so toasts\n"+
			"set after t=500ms will never auto-dismiss — original bug recurs.\n"+
			"Fix: handler must re-arm the tick unconditionally, matching\n"+
			"the tickAgentStatus pattern at model.go:483-488.")
	}
}