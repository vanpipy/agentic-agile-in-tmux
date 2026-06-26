// theme.go — `awp theme list` and `awp theme set <name>` (Phase 5).
package awp

import (
	"fmt"
	"sort"

	"github.com/pi/awp/internal/config"
	"github.com/spf13/cobra"
)

var themeCmd = &cobra.Command{
	Use:   "theme",
	Short: "Manage UI themes (Phase 5)",
}

var themeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available themes",
	RunE: func(cmd *cobra.Command, args []string) error {
		themes := config.BuiltinThemes
		// Sort by name for stable output
		names := make([]string, 0, len(themes))
		for name := range themes {
			names = append(names, name)
		}
		sort.Strings(names)

		fmt.Printf("Available themes (%d):\n\n", len(names))
		for _, name := range names {
			t := themes[name]
			c := t.Colors
			fmt.Printf("  %-25s  %s\n", name, t.Name)
			fmt.Printf("    primary:   %s\n", c.Primary)
			fmt.Printf("    secondary: %s\n", c.Secondary)
			fmt.Printf("    text:      %s\n", c.Text)
		}
		// Show default
		cfg, _ := config.Load("")
		if cfg != nil && cfg.UI.Theme != "" {
			fmt.Printf("\nCurrent: %s\n", cfg.UI.Theme)
		} else {
			fmt.Printf("\nCurrent: (default = first alphabetically: %s)\n", names[0])
		}
		return nil
	},
}

var themeSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Set the active theme",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if _, ok := config.BuiltinThemes[name]; !ok {
			return fmt.Errorf("unknown theme: %s (run 'awp theme list')", name)
		}
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg.UI.Theme = name
		if err := cfg.Save(""); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("Theme set to: %s\n", name)
		fmt.Println("Restart awp for changes to take effect.")
		return nil
	},
}

var themeCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the active theme",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		name := cfg.UI.Theme
		if name == "" {
			// Pick the first alphabetically
			names := make([]string, 0, len(config.BuiltinThemes))
			for n := range config.BuiltinThemes {
				names = append(names, n)
			}
			sort.Strings(names)
			name = names[0]
		}
		fmt.Println(name)
		return nil
	},
}

func init() {
	themeCmd.AddCommand(themeListCmd, themeSetCmd, themeCurrentCmd)
	rootCmd.AddCommand(themeCmd)
}
