// doctor.go — `awp doctor` CLI subcommand (Phase 5).
//
// `awp doctor [--verbose] [--fix]` runs self-diagnostics and reports
// results. Exits 0 if all checks pass, 1 otherwise.
package awp

import (
	"fmt"
	"os"

	"github.com/pi/awp/internal/doctor"
	"github.com/spf13/cobra"
)

var doctorVerbose bool
var doctorFix bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run self-diagnostics (Phase 5)",
	Long: `Run a series of diagnostic checks on your environment and report
which pass/fail. Use this to debug "why isn't awp working?" issues.

Examples:
  awp doctor             # run all checks, show failures only
  awp doctor --verbose   # show all check messages
  awp doctor --fix       # auto-fix fixable issues (e.g. create config dir)

Exit code is 0 if all checks pass, 1 otherwise.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := doctor.NewRunner()
		r.Fix = doctorFix
		res, err := r.Run()
		if err != nil {
			return fmt.Errorf("doctor: %w", err)
		}
		fmt.Print(res.Format(doctorVerbose))
		if !res.AllOK {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().BoolVarP(&doctorVerbose, "verbose", "v", false, "Show all check messages, not just failures")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Auto-fix fixable issues (e.g. create missing config dir)")
}
