package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
)

func newOpenCodeCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "opencode <name>",
		Short: "Attach to the OpenCode server running on a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpenCode(cmd, args, *dataDir, log)
		},
	}

	cmd.Flags().Int("port", 4096, "local port to forward to the remote OpenCode server")
	cmd.Flags().String("dir", "", "working directory for the OpenCode TUI on the remote box")
	cmd.Flags().StringP("session", "s", "", "session ID to continue")

	wait := cmd.Flags().String("wait", "", `wait for SSH to become ready (optional timeout, default "5m")`)
	_ = wait
	cmd.Flags().Lookup("wait").NoOptDefVal = "5m"

	return cmd
}

// remoteOpenCodePort is the port that the opencode-serve systemd user service
// listens on inside the VM. This is hardcoded in the cloud-init template.
const remoteOpenCodePort = 4096

func runOpenCode(cmd *cobra.Command, args []string, dataDir string, log *slog.Logger) error {
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

	localPort, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("reading --port flag: %w", err)
	}

	// --wait: poll until SSH is reachable before proceeding.
	waitFlag, err := getFlag(cmd, "wait")
	if err != nil {
		return err
	}

	if waitFlag != "" {
		timeout, err := time.ParseDuration(waitFlag)
		if err != nil {
			return fmt.Errorf("invalid --wait duration: %w", err)
		}

		if err := waitForSSH(cmd.Context(), keyPath, st.IP, st.SSHPort, timeout, log); err != nil {
			return err
		}
	}

	// Build the SSH tunnel command. The opencode server is managed by a
	// systemd user service on the VM, so we only need port forwarding (-N).
	localForward := fmt.Sprintf("%d:127.0.0.1:%d", localPort, remoteOpenCodePort)

	sshArgs := []string{
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", st.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-N",
		"-L", localForward,
		fmt.Sprintf("%s@%s", state.DefaultUser, st.IP),
	}

	log.Debug("SSH tunnel arguments", "args", strings.Join(append([]string{"ssh"}, sshArgs...), " "))

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("finding ssh binary: %w", err)
	}

	tunnelCmd := exec.CommandContext(cmd.Context(), sshPath, sshArgs...)
	tunnelCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := tunnelCmd.Start(); err != nil {
		return fmt.Errorf("starting SSH tunnel: %w", err)
	}

	// Ensure the tunnel process is cleaned up on exit.
	defer func() {
		if tunnelCmd.Process != nil {
			log.Debug("killing SSH tunnel process")
			_ = tunnelCmd.Process.Kill()
			_ = tunnelCmd.Wait()
		}
	}()

	// Wait for the remote opencode server to become reachable through the tunnel.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/global/health", localPort)
	if err := waitForHealth(cmd.Context(), healthURL, 2*time.Minute, tunnelCmd, log); err != nil {
		return err
	}

	// Find the local opencode binary.
	opencodePath, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("finding opencode binary: %w (is opencode installed locally?)", err)
	}

	attachURL := fmt.Sprintf("http://127.0.0.1:%d", localPort)

	attachArgs := []string{"attach", attachURL}

	dir, err := getFlag(cmd, "dir")
	if err != nil {
		return err
	}

	if dir != "" {
		attachArgs = append(attachArgs, "--dir", dir)
	}

	session, err := getFlag(cmd, "session")
	if err != nil {
		return err
	}

	if session != "" {
		attachArgs = append(attachArgs, "--session", session)
	}

	log.Debug("attaching to opencode server", "url", attachURL, "args", attachArgs)

	// Set up signal forwarding so ctrl+c goes to the attach process, not us.
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	attachCmd := exec.CommandContext(ctx, opencodePath, attachArgs...)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr

	if err := attachCmd.Run(); err != nil {
		// If the user ctrl+c'd or the context was cancelled, that's not an error.
		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf("opencode attach: %w", err)
	}

	return nil
}

// waitForHealth polls a URL until it returns HTTP 200 or the timeout expires.
// It also watches the tunnel process — if it exits early, we return immediately
// with an error instead of polling until timeout.
func waitForHealth(ctx context.Context, url string, timeout time.Duration, tunnel *exec.Cmd, log *slog.Logger) error {
	const pollInterval = 2 * time.Second

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}

	// Channel that closes if the tunnel process exits early.
	tunnelDone := make(chan struct{})
	go func() {
		_ = tunnel.Wait()
		close(tunnelDone)
	}()

	fmt.Printf("Waiting for OpenCode server...")

	for {
		if time.Now().After(deadline) {
			fmt.Println()
			return fmt.Errorf("OpenCode server not ready after %s", timeout)
		}

		if ctx.Err() != nil {
			fmt.Println()
			return ctx.Err()
		}

		select {
		case <-tunnelDone:
			fmt.Println()
			return fmt.Errorf("SSH tunnel exited before OpenCode server became ready")
		default:
		}

		log.Debug("polling OpenCode health", "url", url)

		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println(" ready")
				return nil
			}
		}

		fmt.Print(".")
		time.Sleep(pollInterval)
	}
}
