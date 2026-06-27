// Package awp implements the awp CLI.
//
// See SYSTEM_DESIGN.md §4 (directory structure) and §9 (CLI) for the
// full subcommand plan. Phase 2 adds ticket + session subcommands and
// makes the no-arg `awp` command launch the TUI.
package awp

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pi/awp/internal/buildinfo"
	"github.com/pi/awp/internal/app"
		"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/observability"
	"github.com/pi/awp/internal/pi"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/ui"
	"github.com/spf13/cobra"
)

const version = "0.0.0-dev"

// debug is a global --debug flag. Read by all subcommands.
var debug bool

// rootCmd is the top-level awp command.
var rootCmd = &cobra.Command{
	Use:   "awp",
	Short: "A pi-native task collaboration board",
	Long: `awp is a TUI kanban for running multiple pi sessions in parallel,
each in its own git worktree, observed and steered from a single interface.

See SYSTEM_DESIGN.md for the full design specification.`,
	Version: version,
	// Phase 2: no-arg `awp` launches the TUI
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

// runTUI is the TUI entry point, called when user runs `awp` with no args.
func runTUI() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	registry, err := project.LoadRegistry()
	if err != nil {
		return fmt.Errorf("load project registry: %w", err)
	}
	store, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		return fmt.Errorf("load ticket store: %w", err)
	}

	// Build the model. awp is pi-only: NewModel takes config + ticket
	// store + project registry. The pi subprocess is started lazily
	// by the spawn flow when the user presses 's'.
	model := ui.NewModel(cfg, store, registry, "", nil)

	// Run the Bubble Tea program
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Handle SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		prog.Quit()
	}()

	// Handle SIGUSR1 (debug) only when --debug is set. Production
	// binaries don't accept SIGUSR1 — keeps the process surface
	// small and avoids stray /tmp/awp-stack-*.txt files.
	if observability.IsDebug() {
		registerSIGUSR1StackDumper()
	}

	_, err = prog.Run()
	return err
}

// registerSIGUSR1StackDumper installs a SIGUSR1 handler that writes
// a full goroutine stack dump to /tmp/awp-stack-<pid>-<ts>.txt
// when the signal is received. Only call this in debug builds
// (e.g., when --debug is set) — production awp does not register
// this handler.
//
// Usage:
//   kill -USR1 <pid>
//     # → writes /tmp/awp-stack-<pid>-<timestamp>.txt
func registerSIGUSR1StackDumper() {
	usrCh := make(chan os.Signal, 1)
	signal.Notify(usrCh, syscall.SIGUSR1)
	go func() {
		for range usrCh {
			buf := make([]byte, 1<<20) // 1MB; large enough for hundreds of goroutines
			n := runtime.Stack(buf, true)
			// Write to a file rather than stderr because Bubble Tea
			// controls the TTY and stderr is invisible to the user
			// in TUI alt-screen mode. The user can inspect the file
			// from another terminal to diagnose deadlocks.
			timeStr := time.Now().Format("20060102-150405")
			path := fmt.Sprintf("/tmp/awp-stack-%d-%s.txt", os.Getpid(), timeStr)
			if err := os.WriteFile(path, buf[:n], 0644); err != nil {
				// Fall back to stderr if file write fails (shouldn't happen)
				fmt.Fprintf(os.Stderr, "awp: failed to write stack dump to %s: %v\n", path, err)
				fmt.Fprintln(os.Stderr, "=== SIGUSR1: goroutine dump (length", n, "bytes) ===")
				os.Stderr.Write(buf[:n])
				fmt.Fprintln(os.Stderr, "=== end dump ===")
			} else {
				// Print short notification (visible if alt-screen not yet active)
				fmt.Fprintf(os.Stderr, "awp: SIGUSR1 received, stack dump written to %s\n", path)
			}
		}
	}()
}

// projectCmd handles `awp project <subcommand>`.
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
}

var projectNewCmd = &cobra.Command{
	Use:   "new [name]",
	Short: "Register a new project from a git repo",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		repoPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get current directory: %w", err)
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		} else {
			// Derive project name from the cwd's basename via filepath.Base.
			// This is portable (handles both / and \ separators) and replaces
			// a hand-written character loop that only handled Unix '/'.
			abs, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get current directory: %w", err)
			}
			name = filepath.Base(abs)
			if err := validateProjectName(name); err != nil {
				return fmt.Errorf("invalid project name derived from CWD: %w", err)
			}
		}
		return app.CreateProject(cfg, name, repoPath)
	},
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		return app.ListProjects()
	},
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <name-or-id>",
	Short: "Delete a project by name or ID prefix",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return app.DeleteProject(args[0])
	},
}

// ticketCmd handles `awp ticket <subcommand>` (Phase 2).
var ticketCmd = &cobra.Command{
	Use:   "ticket",
	Short: "Manage tickets (non-interactive)",
}

var ticketListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tickets",
	RunE: func(cmd *cobra.Command, args []string) error {
		registry, err := project.LoadRegistry()
		if err != nil {
			return err
		}
		store, err := project.LoadGlobalTicketStore(registry)
		if err != nil {
			return err
		}
		for _, t := range store.All() {
			fmt.Printf("%s [%s] [%s] %s\n", t.ID[:8], t.Status, t.PiState, t.Title)
		}
		return nil
	},
}

// sessionCmd handles `awp session <subcommand>` (Phase 2).
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage pi sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list [project-path]",
	Short: "List pi sessions for a project (or all if no path given)",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cwd string
		if len(args) > 0 {
			cwd = args[0]
		} else {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("get current directory: %w", err)
			}
		}
		store, err := pi.NewSessionStore("")
		if err != nil {
			return err
		}
		sessions, err := store.List(cwd)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			fmt.Println("No pi sessions found")
			return nil
		}
		fmt.Printf("%-12s %-19s %5s %5s  %s\n",
			"ID", "STARTED", "MSGS", "TOOLS", "CWD")
		fmt.Println(strings.Repeat("-", 80))
		for _, s := range sessions {
			shortID := s.ID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			fmt.Printf("%-12s %-19s %5d %5d  %s\n",
				shortID,
				s.Timestamp.Local().Format("2006-01-02 15:04:05"),
				s.MessageCount,
				s.ToolCount,
				s.CWD,
			)
		}
		// Phase 3 audit fix: surface skipped files so the user knows
		// something went wrong. Previous behavior was silent skip.
		if skipped := store.ListSkipped(); skipped > 0 {
			fmt.Fprintf(os.Stderr, "warning: %d session file(s) skipped (corrupt or in-progress). Run 'awp doctor' for details.\n", skipped)
		}
		return nil
	},
}

var sessionShowCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Print details of a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := pi.NewSessionStore("")
		if err != nil {
			return err
		}
		info, ok := store.FindByID(args[0])
		if !ok {
			return fmt.Errorf("session not found: %s", args[0])
		}
		fmt.Printf("ID:        %s\n", info.ID)
		fmt.Printf("Started:   %s\n", info.Timestamp.Local().Format(time.RFC3339))
		fmt.Printf("CWD:       %s\n", info.CWD)
		fmt.Printf("Model:     %s/%s\n", info.ModelProvider, info.ModelID)
		fmt.Printf("Thinking:  %s\n", info.ThinkingLevel)
		fmt.Printf("Messages:  %d\n", info.MessageCount)
		fmt.Printf("Tools:     %d\n", info.ToolCount)
		if info.ParentID != "" {
			fmt.Printf("Parent:    %s\n", info.ParentID)
		}
		if info.FirstPrompt != "" {
			fmt.Printf("\nFirst prompt:\n  %s\n", info.FirstPrompt)
		}
		if info.LastAssistant != "" {
			fmt.Printf("\nLast assistant:\n  %s\n", info.LastAssistant)
		}

		// CWD existence check
		if _, err := os.Stat(info.CWD); os.IsNotExist(err) {
			fmt.Printf("\n⚠  CWD no longer exists: %s\n", info.CWD)
		}
		return nil
	},
}

var sessionResumeCmd = &cobra.Command{
	Use:   "resume <session-id>",
	Short: "Start awp with the given session resumed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := pi.NewSessionStore("")
		if err != nil {
			return err
		}
		info, ok := store.FindByID(args[0])
		if !ok {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if _, err := os.Stat(info.CWD); os.IsNotExist(err) {
			return fmt.Errorf("session CWD no longer exists: %s\nUse 'awp session fork %s' to start fresh.: %w", info.CWD, args[0], err)
		}
		// Phase 3 minimal resume: print instructions.
		// Full resume would require pre-loading the ticket into the TUI,
		// which is non-trivial without major refactoring. We give the
		// user a clear next step.
		fmt.Printf("Session: %s\n", info.ID)
		fmt.Printf("CWD:     %s\n", info.CWD)
		fmt.Printf("\nResume in TUI:\n  Run 'awp' from %s, then press S to pick this session.\n", info.CWD)
		fmt.Printf("Or directly:\n  cd %s && pi --session %s\n", info.CWD, info.ID)
		return nil
	},
}

var sessionForkCmd = &cobra.Command{
	Use:   "fork <session-id>",
	Short: "Create a new ticket that resumes a session (in a fresh worktree)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := pi.NewSessionStore("")
		if err != nil {
			return err
		}
		info, ok := store.FindByID(args[0])
		if !ok {
			return fmt.Errorf("session not found: %s", args[0])
		}
		fmt.Printf("Fork session: %s\n", info.ID)
		fmt.Printf("Parent CWD:   %s\n", info.CWD)
		fmt.Printf("\nTo fork:\n  1. Create worktree: git -C %s worktree add ../<branch>-fork <branch>\n", info.CWD)
		fmt.Printf("  2. From the worktree: pi --fork --from %s\n", info.ID)
		fmt.Printf("\n(Phase 3 minimal: prints instructions. Full auto-fork is Phase 5.)\n")
		return nil
	},
}


// versionCmd prints the version.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print awp version (commit, build date, go version)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(buildinfo.String())
		return nil
	},
}


func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging to stderr")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		observability.Init(debug)
		observability.Debug("awp starting", "version", version, "args", os.Args[1:])
	}
	projectCmd.AddCommand(projectNewCmd, projectListCmd, projectDeleteCmd)
	ticketCmd.AddCommand(ticketListCmd)
	sessionCmd.AddCommand(sessionListCmd, sessionShowCmd, sessionResumeCmd, sessionForkCmd)
	rootCmd.AddCommand(projectCmd, ticketCmd, sessionCmd, doctorCmd, versionCmd)
}

// Execute runs the root command and returns any error.
// Called by main.go. Initializes the observability logger via
// PersistentPreRun (after flag parsing) so all subcommands can use it.
func Execute() error {
	return rootCmd.Execute()
}

// validateProjectName enforces the project name contract:
//   - 1-256 characters (defense against 10MB name strings bloating projects.json)
//   - No control characters (defense against log injection / UI corruption)
//   - No whitespace-only names (empty after trim)
//
// CASTRATION-3 from post-P3P4 audit: previously names were stored verbatim.
// Length cap is generous (256 chars); normal names are <50.
func validateProjectName(name string) error {
	const maxLen = 256
	if len(name) == 0 {
		return fmt.Errorf("project name must not be empty")
	}
	if len(name) > maxLen {
		return fmt.Errorf("project name too long (%d chars; max %d)", len(name), maxLen)
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("project name must not be only whitespace")
	}
	for i, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("project name contains control character at position %d (0x%02x)", i, r)
		}
	}
	return nil
}
