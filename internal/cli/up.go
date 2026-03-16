package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/voidfunktion/ocbox/internal/cloudinit"
	"github.com/voidfunktion/ocbox/internal/datadir"
	"github.com/voidfunktion/ocbox/internal/provider"
	_ "github.com/voidfunktion/ocbox/internal/provider/hetzner"
	"github.com/voidfunktion/ocbox/internal/sshkey"
	"github.com/voidfunktion/ocbox/internal/state"
)

func init() {
	upCmd := &cobra.Command{
		Use:   "up <name>",
		Short: "Create or start a codebox",
		Args:  cobra.ExactArgs(1),
		RunE:  runUp,
	}

	upCmd.Flags().String("provider", "hetzner", "cloud provider to use")
	upCmd.Flags().String("hetzner-server-type", "cx33", "Hetzner server type")
	upCmd.Flags().String("hetzner-location", "hel1", "Hetzner datacenter location")
	upCmd.Flags().String("hetzner-image", "fedora-43", "Hetzner OS image")
	upCmd.Flags().Bool("tailscale", false, "enable TailScale setup on the VM")

	rootCmd.AddCommand(upCmd)
}

func runUp(cmd *cobra.Command, args []string) error {
	name := args[0]
	boxDir := datadir.BoxDir(DataDir, name)
	stateFile := state.StatePath(boxDir)

	var st *state.BoxState

	_, err := os.Stat(stateFile)
	if err == nil {
		// State file exists: resume existing box.
		st, err = state.Load(stateFile)
		if err != nil {
			return fmt.Errorf("loading state: %w", err)
		}

		p, err := provider.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(DataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		st, err = p.Up(cmd.Context(), st, pubKey, nil)
		if err != nil {
			return fmt.Errorf("bringing up box: %w", err)
		}

		if err := state.Save(stateFile, st); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	} else {
		// New box.
		if err := datadir.EnsureBoxDir(DataDir, name); err != nil {
			return fmt.Errorf("creating box directory: %w", err)
		}

		if err := sshkey.Generate(datadir.SSHDir(DataDir, name)); err != nil {
			return fmt.Errorf("generating SSH key: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(DataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		// Build cloud-init user-data.
		ciCfg := cloudinit.Config{SSHPubKey: pubKey}
		tailscale, err := cmd.Flags().GetBool("tailscale")
		if err != nil {
			return fmt.Errorf("reading --tailscale flag: %w", err)
		}
		if tailscale {
			authKey := os.Getenv("TAILSCALE_AUTHKEY")
			if authKey == "" {
				return fmt.Errorf("--tailscale requires TAILSCALE_AUTHKEY environment variable to be set")
			}
			ciCfg.TailScaleAuth = authKey
		}
		userData, err := cloudinit.Generate(ciCfg)
		if err != nil {
			return fmt.Errorf("generating cloud-init: %w", err)
		}

		now := time.Now()
		st = &state.BoxState{
			Name:      name,
			Provider:  mustGetFlag(cmd, "provider"),
			Status:    "unknown",
			CreatedAt: now,
			UpdatedAt: now,
		}

		p, err := provider.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		opts := map[string]string{
			"server-type": mustGetFlag(cmd, "hetzner-server-type"),
			"location":    mustGetFlag(cmd, "hetzner-location"),
			"image":       mustGetFlag(cmd, "hetzner-image"),
			"user-data":   userData,
		}

		st, err = p.Up(cmd.Context(), st, pubKey, opts)
		if err != nil {
			return fmt.Errorf("bringing up box: %w", err)
		}

		if err := state.Save(stateFile, st); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}

		// Wait for cloud-init to finish and SSH key auth to work.
		keyPath := sshkey.PrivateKeyPath(datadir.SSHDir(DataDir, name))
		if err := waitForSSH(cmd.Context(), keyPath, st.IP, st.SSHPort); err != nil {
			return err
		}
	}

	fmt.Printf("Name:     %s\nProvider: %s\nStatus:   %s\nIP:       %s\nSSH Port: %d\n",
		st.Name, st.Provider, st.Status, st.IP, st.SSHPort)

	return nil
}

func mustGetFlag(cmd *cobra.Command, name string) string {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("flag %q not defined: %v", name, err))
	}
	return v
}

// waitForSSH polls the VM until an SSH connection with key auth succeeds.
// This ensures cloud-init has finished setting up the dev user and
// authorized_keys before codebox up returns.
func waitForSSH(ctx context.Context, keyPath, ip string, port int) error {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            "dev",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))

	const (
		pollInterval = 5 * time.Second
		maxWait      = 5 * time.Minute
	)

	deadline := time.Now().Add(maxWait)
	fmt.Printf("Waiting for SSH to become ready...")
	for {
		if time.Now().After(deadline) {
			fmt.Println()
			return fmt.Errorf("SSH not ready after %s", maxWait)
		}
		if ctx.Err() != nil {
			fmt.Println()
			return ctx.Err()
		}

		conn, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			_ = conn.Close()
			fmt.Println(" ready")
			return nil
		}

		fmt.Print(".")
		time.Sleep(pollInterval)
	}
}
