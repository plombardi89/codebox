package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
)

func newSSHCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh <name>",
		Short: "SSH into a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSSH(cmd, args, *dataDir, log)
		},
	}

	cmd.Flags().Bool("manual", false, "print the ssh command instead of executing it")

	wait := cmd.Flags().String("wait", "", `wait for SSH to become ready (optional timeout, default "5m")`)
	_ = wait
	cmd.Flags().Lookup("wait").NoOptDefVal = "5m"

	return cmd
}

func runSSH(cmd *cobra.Command, args []string, dataDir string, log *slog.Logger) error {
	name := args[0]
	boxDir := datadir.BoxDir(dataDir, name)
	stateFile := state.Path(boxDir)

	st, err := state.Load(stateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if st.Status != state.StatusUp {
		return fmt.Errorf("codebox %s is not running", name)
	}

	keyPath := sshkey.PrivateKeyPath(datadir.SSHDir(dataDir, name))

	manual, err := getBoolFlag(cmd, "manual")
	if err != nil {
		return err
	}

	// --wait: poll until SSH is reachable before connecting.
	waitFlag, err := getFlag(cmd, "wait")
	if err != nil {
		return err
	}

	if waitFlag != "" && !manual {
		timeout, err := time.ParseDuration(waitFlag)
		if err != nil {
			return fmt.Errorf("invalid --wait duration: %w", err)
		}

		if err := waitForSSH(cmd.Context(), keyPath, st.IP, st.SSHPort, timeout, log); err != nil {
			return err
		}
	}

	sshArgs := []string{
		"ssh",
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", st.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", state.DefaultUser, st.IP),
	}

	log.Debug("SSH arguments", "args", strings.Join(sshArgs, " "))

	if manual {
		fmt.Println(strings.Join(sshArgs, " "))
		return nil
	}

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("finding ssh binary: %w", err)
	}

	return syscall.Exec(sshPath, sshArgs, os.Environ())
}
