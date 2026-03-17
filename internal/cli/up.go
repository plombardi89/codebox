package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/plombardi89/codebox/internal/cloudinit"
	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/logging"
	"github.com/plombardi89/codebox/internal/provider"
	_ "github.com/plombardi89/codebox/internal/provider/azure"
	_ "github.com/plombardi89/codebox/internal/provider/hetzner"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
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
	upCmd.Flags().String("azure-vm-size", "Standard_B2s", "Azure VM size")
	upCmd.Flags().String("azure-location", "westeurope", "Azure region")
	upCmd.Flags().Bool("tailscale", false, "enable TailScale setup on the VM")

	rootCmd.AddCommand(upCmd)
}

func runUp(cmd *cobra.Command, args []string) error {
	log := logging.Get()

	name := args[0]
	boxDir := datadir.BoxDir(DataDir, name)
	stateFile := state.StatePath(boxDir)

	var st *state.BoxState

	_, err := os.Stat(stateFile)
	if err == nil {
		// State file exists: resume existing box.
		log.Info("loading existing box state", "name", name)
		st, err = state.Load(stateFile)
		if err != nil {
			return fmt.Errorf("loading state: %w", err)
		}

		log.Debug("provider from state", "provider", st.Provider)
		p, err := provider.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(DataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		log.Info("calling provider.Up", "provider", st.Provider, "name", name)
		st, err = p.Up(cmd.Context(), st, pubKey, nil)
		if err != nil {
			return fmt.Errorf("bringing up box: %w", err)
		}

		if err := state.Save(stateFile, st); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	} else {
		// New box.
		log.Info("creating new box", "name", name)

		if err := datadir.EnsureBoxDir(DataDir, name); err != nil {
			return fmt.Errorf("creating box directory: %w", err)
		}

		log.Info("generating SSH key", "name", name)
		if err := sshkey.Generate(datadir.SSHDir(DataDir, name)); err != nil {
			return fmt.Errorf("generating SSH key: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(DataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		// Build cloud-init user-data.
		log.Info("generating cloud-init", "name", name)
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

		providerName := mustGetFlag(cmd, "provider")
		log.Debug("provider selected", "provider", providerName)

		now := time.Now()
		st = &state.BoxState{
			Name:      name,
			Provider:  providerName,
			Status:    "unknown",
			CreatedAt: now,
			UpdatedAt: now,
		}

		p, err := provider.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		opts := map[string]string{"user-data": userData}
		switch providerName {
		case "hetzner":
			opts["server-type"] = mustGetFlag(cmd, "hetzner-server-type")
			opts["location"] = mustGetFlag(cmd, "hetzner-location")
			opts["image"] = mustGetFlag(cmd, "hetzner-image")
		case "azure":
			opts["vm-size"] = mustGetFlag(cmd, "azure-vm-size")
			opts["location"] = mustGetFlag(cmd, "azure-location")
		}
		log.Debug("provider opts", "opts", fmt.Sprintf("%v", opts))

		log.Info("calling provider.Up", "provider", st.Provider, "name", name)
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
	log := logging.Get()

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
	attempt := 0
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

		attempt++
		log.Debug("waitForSSH attempt", "attempt", attempt, "addr", addr)
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
