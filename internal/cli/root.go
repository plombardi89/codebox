package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/provider"
)

// NewRootCommand builds and returns the fully-assembled CLI command tree.
// The caller is responsible for registering providers in reg before executing
// the returned command.
func NewRootCommand(reg *provider.Registry, log *slog.Logger, levelVar *slog.LevelVar) *cobra.Command {
	var (
		dataDir string
		verbose bool
	)

	rootCmd := &cobra.Command{
		Use:   "codebox",
		Short: "Manage remote development environments",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if verbose {
				levelVar.Set(slog.LevelDebug)
			}

			return nil
		},
	}

	defaultDataDir := os.Getenv("CODEBOX_DATA_DIR")
	if defaultDataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			defaultDataDir = filepath.Join(".", ".codebox")
		} else {
			defaultDataDir = filepath.Join(home, ".codebox")
		}
	}

	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", defaultDataDir, "path to codebox data directory")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	rootCmd.AddCommand(
		newUpCmd(reg, &dataDir, log),
		newDownCmd(reg, &dataDir, log),
		newSSHCmd(&dataDir, log),
		newOpenCodeCmd(&dataDir, log),
		newSyncCmd(&dataDir, log),
		newFileSyncCmd(&dataDir, log),
		newLsCmd(&dataDir),
	)

	return rootCmd
}

// Execute builds the command tree with the given registry and logger and runs it.
func Execute(reg *provider.Registry, log *slog.Logger, levelVar *slog.LevelVar) {
	if err := NewRootCommand(reg, log, levelVar).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
