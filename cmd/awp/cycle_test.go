// cycle_test.go — tests for the `awp cycle` headless sub-command.
//
// Most of the sub-command logic lives in runCycle which is hard to
// unit-test (calls os.Exit). The pure logic is in verdictExitCode
// which is straightforward.

package awp

import (
	"errors"
	"testing"

	"github.com/pi/awp/internal/wiking"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestVerdictExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil (accepted)", nil, 0},
		{"cancelled", wiking.ErrCancelled, 0},
		{"phase_timeout", wiking.ErrPhaseTimeout, 1},
		{"no_progress", wiking.ErrNoProgress, 2},
		{"generic error", errors.New("some error"), 3},
		{"wrapped phase_timeout", errors.Join(wiking.ErrPhaseTimeout, errors.New("extra")), 1},
		{"wrapped no_progress", errors.Join(wiking.ErrNoProgress, errors.New("extra")), 2},
		{"wrapped cancelled", errors.Join(wiking.ErrCancelled, errors.New("user signal")), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := verdictExitCode(tc.err)
			if got != tc.want {
				t.Errorf("verdictExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestCycleCmdRegistered verifies that adding the cycleCmd in init()
// landed it on the root command. This catches accidental removal
// or rename.
func TestCycleCmdRegistered(t *testing.T) {
	root := rootCmd
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "cycle" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("rootCmd has no 'cycle' subcommand")
	}
	if found.Use != "cycle <article-stem>" {
		t.Errorf("cycleCmd.Use = %q; want 'cycle <article-stem>'", found.Use)
	}
	// Flags expected. Iterate the FlagSet's pflag.Flag slice.
	wantFlags := map[string]bool{"wiki": false, "force": false, "threshold": false}
	found.Flags().VisitAll(func(f *pflag.Flag) {
		if _, ok := wantFlags[f.Name]; ok {
			wantFlags[f.Name] = true
		}
	})
	for name, present := range wantFlags {
		if !present {
			t.Errorf("cycleCmd missing --%s flag", name)
		}
	}
}
