// cmd/awp/cycle.go — `awp cycle <article-stem>` headless sub-command.
//
// Implements SYSTEM_DESIGN.md §18.4 entry point: the dumb postman
// as a CLI command. One round per invocation: wiking writes
// article-{N}.md, coding reviews article-{N}-feedback-{N}.md,
// cycle iterates until score >= threshold. On acceptance, the
// article is synced to canonical "<article>.md" (drop .md? see
// below for the stripping note).
//
// Note: the article stem as passed to the CLI is the article
// name WITHOUT the .md extension. Each round writes article-{N}.md
// (and the canonical article.md). The user provides a single name
// and the cycle numbers the rounds internally.
package awp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/observability"
	"github.com/pi/awp/internal/wiking"

	"github.com/spf13/cobra"
)

var cycleWikiDir string
var cycleForceAccept bool
var cycleThreshold int

var cycleCmd = &cobra.Command{
	Use:   "cycle <article-stem>",
	Short: "Run a wiking↔coding 2-cycle to acceptance (headless)",
	Long: `Run the dumb postman (SYSTEM_DESIGN.md §18) headlessly.

The cycle invokes the wiking pi role to draft article-N.md, then the
coding pi role to review it (article-N-feedback-N.md). It loops
until the coding score is >= --threshold (default from config),
then syncs the winning plan to canonical '<wiki>/<article>.md'.

Examples:
  awp cycle extend-update
  awp cycle --wiki ~/wiki extend-update
  awp cycle --threshold 75 extend-update
  awp cycle --force extend-update   # bypass score, accept latest draft

Exit codes:
  0  cycle accepted (score >= threshold, article synced)
     OR cycle cancelled (clean exit on SIGINT/SIGTERM)
  1  phase timed out (wiking or coding exceeded its phase timeout)
  2  no progress detected (MaxNoProgress consecutive idle ticks)
  3  other error`,
	Args: cobra.ExactArgs(1),
	RunE: runCycle,
}

func init() {
	cycleCmd.Flags().StringVar(&cycleWikiDir, "wiki", "",
		"Wiki directory where articles live (default: cwd)")
	cycleCmd.Flags().BoolVar(&cycleForceAccept, "force", false,
		"Bypass score check: accept latest wiking draft as-is")
	cycleCmd.Flags().IntVar(&cycleThreshold, "threshold", 0,
		"Acceptance threshold 0-100 (default from ~/.config/awp/config.json)")
	rootCmd.AddCommand(cycleCmd)
}

func runCycle(cmd *cobra.Command, args []string) error {
	article := args[0]

	// Load config; fall back to defaults if missing/invalid.
	cfg, err := config.Load("")
	if err != nil {
		observability.Warn("cycle: config load failed, using defaults", "err", err)
		cfg = config.DefaultConfig()
	}

	// Wiki directory resolution.
	wikiDir := cycleWikiDir
	if wikiDir == "" {
		wd, werr := os.Getwd()
		if werr != nil {
			return fmt.Errorf("getwd: %w", werr)
		}
		wikiDir = wd
	}

	// AWPHome — parent for ~/.awp/cycle/<run-id>/events.jsonl.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home: %w", err)
	}
	awpHome := filepath.Join(home, ".awp")

	// Run ID: deterministic + unique per invocation. Sanitize
	// the article stem so a '/' or other unsafe char in the
	// user's input can't escape the cycle/ subdirectory (see
	// wiking.SanitizeRunID for the rationale).
	runID := fmt.Sprintf("%s-%d", wiking.SanitizeRunID(article), time.Now().Unix())

	// Threshold: --threshold flag overrides config; config default applies otherwise.
	threshold := cycleThreshold
	if threshold <= 0 {
		threshold = cfg.Cycle.Threshold
	}

	// Build the cycle config from the awp config.
	runCfg := wiking.Config{
		WikiDir:        wikiDir,
		RunID:          runID,
		AWPHome:        awpHome,
		Threshold:      threshold,
		IdleInterval:   cfg.Cycle.IdleInterval,
		WikingInterval: cfg.Cycle.WikingInterval,
		CodingInterval: cfg.Cycle.CodingInterval,
		WikingTimeout:  cfg.Cycle.WikingTimeout,
		CodingTimeout:  cfg.Cycle.CodingTimeout,
		MaxNoProgress:  cfg.Cycle.MaxNoProgress,
		Binary:         cfg.Pi.Command,
		Wiking: wiking.RoleBinding{
			Prompt: cfg.Wiking.Prompt,
			CWD:    wikiDir,
		},
		Coding: wiking.RoleBinding{
			Prompt: cfg.Coding.Prompt,
			CWD:    wikiDir,
		},
	}

	cyc, err := wiking.New(runCfg)
	if err != nil {
		return fmt.Errorf("cycle init: %w", err)
	}

	// Signal handling — SIGINT/SIGTERM cancels the cycle for a
	// clean exit (exit code 0). cycleForceAccept skips signal
	// handling: it's a no-spawn path used by tests and one-shot
	// accept scripts.
	ctx := context.Background()
	if !cycleForceAccept {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go handleCycleSignal(cancel)
	}

	observability.Info("cycle: starting", "article", article, "wiki", wikiDir,
		"run_id", runID, "threshold", threshold)

	err = cyc.Run(ctx)

	os.Exit(verdictExitCode(err))
	return nil // unreachable
}

// handleCycleSignal cancels the cycle's context on SIGINT/SIGTERM.
func handleCycleSignal(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	if _, ok := <-sigCh; ok {
		cancel()
	}
}

// verdictExitCode maps a cycle error to a process exit code per the
// documented contract in cycleCmd.Long. Pure function — testable
// without invoking cobra or os.Exit.
func verdictExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, wiking.ErrPhaseTimeout):
		return 1
	case errors.Is(err, wiking.ErrNoProgress):
		return 2
	case errors.Is(err, wiking.ErrCancelled):
		return 0 // cancellation is a normal/clean exit
	default:
		return 3
	}
}
