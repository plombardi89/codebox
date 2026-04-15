package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/plombardi89/codebox/internal/cloudinit"
	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/profile"
	"github.com/plombardi89/codebox/internal/provider"
	azureprovider "github.com/plombardi89/codebox/internal/provider/azure"
	"github.com/plombardi89/codebox/internal/sshconfig"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
)

func newUpCmd(reg *provider.Registry, dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up <name>",
		Short: "Create or start a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd, args, reg, *dataDir, log)
		},
	}

	cmd.Flags().String("provider", "hetzner", "cloud provider to use")
	cmd.Flags().String("hetzner-server-type", "cx33", "Hetzner server type")
	cmd.Flags().String("hetzner-location", "hel1", "Hetzner datacenter location")
	cmd.Flags().String("hetzner-image", "fedora-43", "Hetzner OS image")
	cmd.Flags().String("azure-vm-size", "standard_d2ads_v6", "Azure VM size")
	cmd.Flags().String("azure-location", "canadacentral", "Azure region")
	cmd.Flags().String("azure-subscription-id", "", "Azure subscription ID (overrides AZURE_SUBSCRIPTION_ID)")
	cmd.Flags().Bool("tailscale", false, "enable TailScale setup on the VM")
	cmd.Flags().Bool("recreate", false, "delete and recreate the box with fresh cloud-init config")
	cmd.Flags().String("profile", "", "box profile name to load from ~/.codebox/profiles/<name>.yaml")

	return cmd
}

// getFlag returns the string value of a flag, or an error if the flag is not defined.
func getFlag(cmd *cobra.Command, name string) (string, error) {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", fmt.Errorf("reading --%s flag: %w", name, err)
	}

	return v, nil
}

// getBoolFlag returns the bool value of a flag, or an error if the flag is not defined.
func getBoolFlag(cmd *cobra.Command, name string) (bool, error) {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		return false, fmt.Errorf("reading --%s flag: %w", name, err)
	}

	return v, nil
}

// buildProviderOpts generates the cloud-init user-data and collects all
// provider-specific flags into an opts map suitable for Provider.Up().
// It is used by both the new-box and resume paths so that a provider can
// recreate a deleted VM with the same configuration.
func buildProviderOpts(cmd *cobra.Command, providerName, dataDir, boxName, pubKey string, extraPackages []string, log *slog.Logger) (map[string]string, error) {
	// Build cloud-init user-data.
	ciCfg := cloudinit.Config{
		SSHPubKey:     pubKey,
		ExtraPackages: extraPackages,
		BoxName:       boxName,
	}

	tailscale, err := getBoolFlag(cmd, "tailscale")
	if err != nil {
		return nil, err
	}

	if tailscale {
		authKey := os.Getenv("TAILSCALE_AUTHKEY")
		if authKey == "" {
			return nil, fmt.Errorf("--tailscale requires TAILSCALE_AUTHKEY environment variable to be set")
		}

		ciCfg.TailScaleAuth = authKey
	}

	userData, err := cloudinit.Generate(ciCfg, log)
	if err != nil {
		return nil, fmt.Errorf("generating cloud-init: %w", err)
	}

	opts := map[string]string{"user-data": userData}

	switch providerName {
	case "hetzner":
		for _, kv := range []struct{ flag, key string }{
			{"hetzner-server-type", "server-type"},
			{"hetzner-location", "location"},
			{"hetzner-image", "image"},
		} {
			v, err := getFlag(cmd, kv.flag)
			if err != nil {
				return nil, err
			}

			opts[kv.key] = v
		}
	case "azure":
		for _, kv := range []struct{ flag, key string }{
			{"azure-vm-size", "vm-size"},
			{"azure-location", "location"},
			{"azure-subscription-id", "subscription-id"},
		} {
			v, err := getFlag(cmd, kv.flag)
			if err != nil {
				return nil, err
			}

			opts[kv.key] = v
		}
		// Pass SSH private key so the Azure provider can store it in Key Vault.
		privKeyData, err := os.ReadFile(sshkey.PrivateKeyPath(datadir.SSHDir(dataDir, boxName)))
		if err != nil {
			return nil, fmt.Errorf("reading private key for Key Vault: %w", err)
		}

		opts["ssh-private-key"] = string(privKeyData)
	}

	return opts, nil
}

// destroyExistingVM deletes only the VM (and its boot disk) for an existing
// box so that runUp can recreate it from scratch with fresh cloud-init config.
// Infrastructure (networks, keys, Key Vault, etc.) and local state are kept.
// If no state file exists, this is a no-op.
func destroyExistingVM(cmd *cobra.Command, reg *provider.Registry, stateFile string, log *slog.Logger) error {
	st, err := state.Load(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to destroy
		}

		return fmt.Errorf("loading state for recreate: %w", err)
	}

	p, err := reg.Get(st.Provider)
	if err != nil {
		return fmt.Errorf("getting provider for recreate: %w", err)
	}

	// Inject Azure subscription ID override if provided.
	azSubID, err := getFlag(cmd, "azure-subscription-id")
	if err != nil {
		return err
	}

	if azSubID != "" {
		st.EnsureProviderData()
		st.ProviderData[azureprovider.KeySubscriptionID] = azSubID
	}

	log.Info("recreate: deleting VM", "name", st.Name)

	if err := p.DestroyVM(cmd.Context(), st); err != nil {
		return fmt.Errorf("recreate: deleting VM: %w", err)
	}

	// Save updated state (provider may have cleared VM-specific keys).
	if err := state.Save(stateFile, st, log); err != nil {
		return fmt.Errorf("recreate: saving state: %w", err)
	}

	return nil
}

// resolveProfilePackages determines which profile to use and returns its
// packages. If the --profile flag is explicitly set it takes precedence;
// otherwise the persisted profile name from state is used. Returns nil when
// no profile applies.
func resolveProfilePackages(cmd *cobra.Command, stateProfile, dataDir string, log *slog.Logger) ([]string, error) {
	profileName, err := getFlag(cmd, "profile")
	if err != nil {
		return nil, err
	}

	// Prefer explicit flag; fall back to what was saved in state.
	if profileName == "" {
		profileName = stateProfile
	}

	if profileName == "" {
		return nil, nil
	}

	prof, err := profile.Load(dataDir, profileName)
	if err != nil {
		return nil, fmt.Errorf("loading profile: %w", err)
	}

	log.Info("using box profile", "profile", profileName, "packages", prof.Packages)

	return prof.Packages, nil
}

func runUp(cmd *cobra.Command, args []string, reg *provider.Registry, dataDir string, log *slog.Logger) error {
	name := args[0]
	boxDir := datadir.BoxDir(dataDir, name)
	stateFile := state.Path(boxDir)

	// --recreate: tear down the existing box so a fresh one is created below.
	recreate, err := getBoolFlag(cmd, "recreate")
	if err != nil {
		return err
	}

	if recreate {
		if err := destroyExistingVM(cmd, reg, stateFile, log); err != nil {
			return err
		}
	}

	var st *state.Box

	_, err = os.Stat(stateFile)
	if err == nil {
		// State file exists: resume existing box.
		log.Info("loading existing box state", "name", name)

		st, err = state.Load(stateFile)
		if err != nil {
			return fmt.Errorf("loading state: %w", err)
		}

		log.Debug("provider from state", "provider", st.Provider)

		p, err := reg.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(dataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		// Resolve profile: prefer explicit --profile flag, fall back to
		// the profile persisted in state (set at creation time).
		extraPackages, err := resolveProfilePackages(cmd, st.Profile, dataDir, log)
		if err != nil {
			return err
		}

		// Build full opts so the provider can recreate a deleted VM.
		// For a normal resume (VM still exists) these extra fields are
		// ignored; for a recreate they are essential.
		upOpts, err := buildProviderOpts(cmd, st.Provider, dataDir, name, pubKey, extraPackages, log)
		if err != nil {
			return err
		}

		log.Info("calling provider.Up", "provider", st.Provider, "name", name)

		st, err = p.Up(cmd.Context(), st, pubKey, upOpts)
		if err != nil {
			return fmt.Errorf("bringing up box: %w", err)
		}

		if err := state.Save(stateFile, st, log); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	} else {
		// New box.
		log.Info("creating new box", "name", name)

		if err := datadir.EnsureBoxDir(dataDir, name); err != nil {
			return fmt.Errorf("creating box directory: %w", err)
		}

		log.Info("generating SSH key", "name", name)

		if err := sshkey.Generate(datadir.SSHDir(dataDir, name), log); err != nil {
			return fmt.Errorf("generating SSH key: %w", err)
		}

		pubKey, err := sshkey.ReadPublicKey(datadir.SSHDir(dataDir, name))
		if err != nil {
			return fmt.Errorf("reading public key: %w", err)
		}

		providerName, err := getFlag(cmd, "provider")
		if err != nil {
			return err
		}

		log.Debug("provider selected", "provider", providerName)

		now := time.Now()
		st = &state.Box{
			Name:      name,
			Provider:  providerName,
			Status:    state.StatusUnknown,
			CreatedAt: now,
			UpdatedAt: now,
		}

		// Load profile packages if --profile was given.
		profileName, err := getFlag(cmd, "profile")
		if err != nil {
			return err
		}

		var extraPackages []string

		if profileName != "" {
			prof, err := profile.Load(dataDir, profileName)
			if err != nil {
				return fmt.Errorf("loading profile: %w", err)
			}

			extraPackages = prof.Packages
			st.Profile = profileName
			log.Info("using box profile", "profile", profileName, "packages", extraPackages)
		}

		p, err := reg.Get(st.Provider)
		if err != nil {
			return fmt.Errorf("getting provider: %w", err)
		}

		opts, err := buildProviderOpts(cmd, providerName, dataDir, name, pubKey, extraPackages, log)
		if err != nil {
			return err
		}

		log.Debug("provider opts", "opts", fmt.Sprintf("%v", opts))

		log.Info("calling provider.Up", "provider", st.Provider, "name", name)

		st, err = p.Up(cmd.Context(), st, pubKey, opts)
		if err != nil {
			return fmt.Errorf("bringing up box: %w", err)
		}

		if err := state.Save(stateFile, st, log); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}

		// Wait for cloud-init to finish and SSH key auth to work.
		keyPath := sshkey.PrivateKeyPath(datadir.SSHDir(dataDir, name))
		if err := waitForSSH(cmd.Context(), keyPath, st.IP, st.SSHPort, 5*time.Minute, log); err != nil {
			return err
		}
	}

	// Update the SSH config entry so the host alias is always current.
	if st.IP != "" {
		if err := sshconfig.WriteBoxEntry(dataDir, name, st.IP, st.SSHPort); err != nil {
			log.Debug("could not update SSH config entry", "error", err)
		}
	}

	fmt.Printf("Name:     %s\nProvider: %s\nStatus:   %s\nIP:       %s\nSSH Port: %d\n",
		st.Name, st.Provider, st.Status, st.IP, st.SSHPort)

	return nil
}

// waitForSSH polls the VM until an SSH connection with key auth succeeds.
// It retries for up to timeout, using a 5-second poll interval.
func waitForSSH(ctx context.Context, keyPath, ip string, port int, timeout time.Duration, log *slog.Logger) error {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            state.DefaultUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))

	const pollInterval = 5 * time.Second

	deadline := time.Now().Add(timeout)
	attempt := 0

	fmt.Printf("Waiting for SSH to become ready...")

	for {
		if time.Now().After(deadline) {
			fmt.Println()
			return fmt.Errorf("SSH not ready after %s", timeout)
		}

		if ctx.Err() != nil {
			fmt.Println()
			return ctx.Err()
		}

		attempt++
		log.Debug("waitForSSH attempt", "attempt", attempt, "addr", addr)

		conn, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				log.Debug("closing SSH probe connection", "error", closeErr)
			}

			fmt.Println(" ready")

			return nil
		}

		fmt.Print(".")
		time.Sleep(pollInterval)
	}
}
