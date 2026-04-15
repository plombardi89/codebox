package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/mutagen"
	"github.com/plombardi89/codebox/internal/sshconfig"
	"github.com/plombardi89/codebox/internal/state"
)

func newFileSyncCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filesync",
		Short: "Manage file sync sessions with a codebox",
	}

	cmd.AddCommand(
		newFileSyncStartCmd(dataDir, log),
		newFileSyncStopCmd(dataDir, log),
		newFileSyncPauseCmd(dataDir, log),
		newFileSyncResumeCmd(dataDir, log),
		newFileSyncStatusCmd(dataDir, log),
		newFileSyncLsCmd(dataDir, log),
	)

	return cmd
}

// loadBoxForSync is a helper that loads box state and verifies the box is
// running. It returns the loaded state or an error.
func loadBoxForSync(dataDir, name string) (*state.Box, error) {
	boxDir := datadir.BoxDir(dataDir, name)
	stateFile := state.Path(boxDir)

	st, err := state.Load(stateFile)
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}

	if st.Status != state.StatusUp {
		return nil, fmt.Errorf("codebox %s is not running", name)
	}

	return st, nil
}

func newFileSyncStartCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <name> <local>:<remote> [<local>:<remote> ...]",
		Short: "Start file sync sessions between local and remote paths",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncStart(cmd, args, *dataDir, log)
		},
	}

	cmd.Flags().String("mode", mutagen.DefaultSyncMode, "mutagen sync mode")

	return cmd
}

func runFileSyncStart(cmd *cobra.Command, args []string, dataDir string, log *slog.Logger) error {
	name := args[0]
	pathArgs := args[1:]

	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	st, err := loadBoxForSync(dataDir, name)
	if err != nil {
		return err
	}

	mode, err := getFlag(cmd, "mode")
	if err != nil {
		return err
	}

	// Ensure the SSH config is set up so mutagen's ssh can reach the box.
	if err := sshconfig.EnsureInclude(dataDir); err != nil {
		return fmt.Errorf("setting up SSH config include: %w", err)
	}

	if err := sshconfig.WriteBoxEntry(dataDir, name, st.IP, st.SSHPort); err != nil {
		return fmt.Errorf("writing SSH config entry: %w", err)
	}

	hostAlias := sshconfig.HostAlias(name)

	for _, arg := range pathArgs {
		localPath, remotePath, err := mutagen.ParsePathPair(arg)
		if err != nil {
			return err
		}

		opts := mutagen.CreateOpts{
			BoxName:    name,
			LocalPath:  localPath,
			RemotePath: remotePath,
			HostAlias:  hostAlias,
			SyncMode:   mode,
		}

		fmt.Printf("Starting sync: %s -> %s\n", localPath, remotePath)

		if err := mutagen.CreateSession(cmd.Context(), opts, log); err != nil {
			return fmt.Errorf("creating sync session for %s: %w", arg, err)
		}
	}

	fmt.Printf("Started %d sync session(s) for %s\n", len(pathArgs), name)

	return nil
}

func newFileSyncStopCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop all file sync sessions for a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncStop(cmd, args, log)
		},
	}
}

func runFileSyncStop(cmd *cobra.Command, args []string, log *slog.Logger) error {
	name := args[0]

	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	if err := mutagen.StopSessions(cmd.Context(), name, log); err != nil {
		return fmt.Errorf("stopping sync sessions: %w", err)
	}

	fmt.Printf("Stopped sync sessions for %s\n", name)

	return nil
}

func newFileSyncPauseCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "pause <name>",
		Short: "Pause all file sync sessions for a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncPause(cmd, args, log)
		},
	}
}

func runFileSyncPause(cmd *cobra.Command, args []string, log *slog.Logger) error {
	name := args[0]

	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	if err := mutagen.PauseSessions(cmd.Context(), name, log); err != nil {
		return fmt.Errorf("pausing sync sessions: %w", err)
	}

	fmt.Printf("Paused sync sessions for %s\n", name)

	return nil
}

func newFileSyncResumeCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "resume <name>",
		Short: "Resume all file sync sessions for a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncResume(cmd, args, log)
		},
	}
}

func runFileSyncResume(cmd *cobra.Command, args []string, log *slog.Logger) error {
	name := args[0]

	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	if err := mutagen.ResumeSessions(cmd.Context(), name, log); err != nil {
		return fmt.Errorf("resuming sync sessions: %w", err)
	}

	fmt.Printf("Resumed sync sessions for %s\n", name)

	return nil
}

func newFileSyncStatusCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Show sync status for a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncStatus(cmd, args, log)
		},
	}
}

func runFileSyncStatus(cmd *cobra.Command, args []string, log *slog.Logger) error {
	name := args[0]

	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	out, err := mutagen.StatusSession(cmd.Context(), name, log)
	if err != nil {
		return fmt.Errorf("getting sync status: %w", err)
	}

	fmt.Print(out)

	return nil
}

func newFileSyncLsCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [<name>]",
		Short: "List file sync sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSyncLs(cmd, args, log)
		},
	}
}

func runFileSyncLs(cmd *cobra.Command, args []string, log *slog.Logger) error {
	if err := mutagen.EnsureInstalled(); err != nil {
		return err
	}

	var boxName string
	if len(args) > 0 {
		boxName = args[0]
	}

	out, err := mutagen.ListSessions(cmd.Context(), boxName, log)
	if err != nil {
		return fmt.Errorf("listing sync sessions: %w", err)
	}

	fmt.Print(out)

	return nil
}
