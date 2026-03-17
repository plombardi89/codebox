package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/logging"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	sshCmd := &cobra.Command{
		Use:   "ssh <name>",
		Short: "SSH into a codebox",
		Args:  cobra.ExactArgs(1),
		RunE:  runSSH,
	}

	sshCmd.Flags().Bool("manual", false, "print the ssh command instead of executing it")

	rootCmd.AddCommand(sshCmd)
}

func runSSH(cmd *cobra.Command, args []string) error {
	log := logging.Get()

	name := args[0]
	boxDir := datadir.BoxDir(DataDir, name)
	stateFile := state.StatePath(boxDir)

	st, err := state.Load(stateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if st.Status != "up" {
		return fmt.Errorf("codebox %s is not running", name)
	}

	keyPath := sshkey.PrivateKeyPath(datadir.SSHDir(DataDir, name))

	sshArgs := []string{
		"ssh",
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", st.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("dev@%s", st.IP),
	}

	log.Debug("SSH arguments", "args", strings.Join(sshArgs, " "))

	manual, _ := cmd.Flags().GetBool("manual")
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
