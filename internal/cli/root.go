package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// DataDir is the resolved data directory path, accessible to subcommands.
var DataDir string

var rootCmd = &cobra.Command{
	Use:   "codebox",
	Short: "Manage remote development environments",
}

func init() {
	defaultDataDir := os.Getenv("CODEBOX_DATA_DIR")
	if defaultDataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			defaultDataDir = filepath.Join(".", ".codebox")
		} else {
			defaultDataDir = filepath.Join(home, ".codebox")
		}
	}

	rootCmd.PersistentFlags().StringVar(&DataDir, "data-dir", defaultDataDir, "path to codebox data directory")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
