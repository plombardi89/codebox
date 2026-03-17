package hetzner

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/plombardi89/codebox/internal/logging"
	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"
)

func init() {
	provider.Register("hetzner", &hetznerProvider{})
}

// hetznerProvider implements provider.Provider using the Hetzner Cloud API.
type hetznerProvider struct{}

func (h *hetznerProvider) Name() string {
	return "hetzner"
}

// getClient creates a new hcloud client from the HCLOUD_TOKEN environment
// variable. It returns an error if the token is not set.
func getClient() (*hcloud.Client, error) {
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HCLOUD_TOKEN environment variable is not set")
	}
	return hcloud.NewClient(hcloud.WithToken(token)), nil
}

// getServer fetches the server identified by the "server_id" entry in
// st.ProviderData.
func getServer(ctx context.Context, client *hcloud.Client, st *state.BoxState) (*hcloud.Server, error) {
	raw, ok := st.ProviderData["server_id"]
	if !ok {
		return nil, fmt.Errorf("server_id not found in provider data")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing server_id %q: %w", raw, err)
	}
	server, _, err := client.Server.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting server %d: %w", id, err)
	}
	if server == nil {
		return nil, fmt.Errorf("server %d not found", id)
	}
	return server, nil
}

// optOrDefault returns opts[key] if present and non-empty, otherwise def.
func optOrDefault(opts map[string]string, key, def string) string {
	if v, ok := opts[key]; ok && v != "" {
		return v
	}
	return def
}

func (h *hetznerProvider) Up(ctx context.Context, st *state.BoxState, pubKey string, opts map[string]string) (*state.BoxState, error) {
	log := logging.Get()

	client, err := getClient()
	if err != nil {
		return nil, err
	}

	serverType := optOrDefault(opts, "server-type", "cx33")
	location := optOrDefault(opts, "location", "hel1")
	image := optOrDefault(opts, "image", "fedora-43")

	// If a server already exists, handle idempotently.
	if _, ok := st.ProviderData["server_id"]; ok {
		log.Debug("server ID found in state", "server_id", st.ProviderData["server_id"])
		server, err := getServer(ctx, client, st)
		if err != nil {
			return nil, err
		}

		switch server.Status {
		case hcloud.ServerStatusRunning:
			// Already running, update state and return.
			log.Debug("server already running", "name", st.Name)
			st.Status = "up"
			st.IP = server.PublicNet.IPv4.IP.String()
			st.SSHPort = 2222
			st.UpdatedAt = time.Now()
			log.Debug("server IP", "ip", st.IP)
			return st, nil

		case hcloud.ServerStatusOff:
			// Power on and wait.
			log.Info("powering on server", "name", st.Name)
			action, _, err := client.Server.Poweron(ctx, server)
			if err != nil {
				return nil, fmt.Errorf("powering on server: %w", err)
			}
			if err := client.Action.WaitFor(ctx, action); err != nil {
				return nil, fmt.Errorf("waiting for power on: %w", err)
			}
			st.Status = "up"
			st.IP = server.PublicNet.IPv4.IP.String()
			st.SSHPort = 2222
			st.UpdatedAt = time.Now()
			log.Debug("server IP", "ip", st.IP)
			return st, nil

		default:
			return nil, fmt.Errorf("server is in unexpected status: %s", server.Status)
		}
	}

	// No existing server -- create from scratch.
	if st.ProviderData == nil {
		st.ProviderData = make(map[string]string)
	}

	// Create or reuse SSH key in Hetzner.
	sshKeyName := fmt.Sprintf("codebox-%s", st.Name)
	sshKey, _, err := client.SSHKey.GetByName(ctx, sshKeyName)
	if err != nil {
		return nil, fmt.Errorf("looking up SSH key: %w", err)
	}
	if sshKey == nil {
		log.Info("creating SSH key", "name", sshKeyName)
		sshKey, _, err = client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
			Name:      sshKeyName,
			PublicKey: pubKey,
		})
		if err != nil {
			return nil, fmt.Errorf("creating SSH key: %w", err)
		}
	} else {
		log.Debug("SSH key already exists", "name", sshKeyName)
	}
	st.ProviderData["ssh_key_id"] = fmt.Sprintf("%d", sshKey.ID)

	// Create server.
	log.Info("creating server", "name", st.Name, "type", serverType, "location", location, "image", image)
	result, _, err := client.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name: st.Name,
		ServerType: &hcloud.ServerType{
			Name: serverType,
		},
		Location: &hcloud.Location{
			Name: location,
		},
		Image: &hcloud.Image{
			Name: image,
		},
		SSHKeys:  []*hcloud.SSHKey{sshKey},
		UserData: opts["user-data"],
	})
	if err != nil {
		return nil, fmt.Errorf("creating server: %w", err)
	}

	// Wait for the create action to complete.
	if err := client.Action.WaitFor(ctx, result.Action); err != nil {
		return nil, fmt.Errorf("waiting for server creation: %w", err)
	}

	// Wait for additional actions (e.g. network setup) that are part of
	// server creation.
	for _, a := range result.NextActions {
		if err := client.Action.WaitFor(ctx, a); err != nil {
			return nil, fmt.Errorf("waiting for server next action: %w", err)
		}
	}

	st.ProviderData["server_id"] = fmt.Sprintf("%d", result.Server.ID)
	st.IP = result.Server.PublicNet.IPv4.IP.String()
	st.SSHPort = 2222
	st.Image = image
	st.Status = "up"
	st.UpdatedAt = time.Now()
	log.Debug("server created", "server_id", result.Server.ID, "ip", st.IP)

	return st, nil
}

func (h *hetznerProvider) Down(ctx context.Context, st *state.BoxState) (*state.BoxState, error) {
	log := logging.Get()

	client, err := getClient()
	if err != nil {
		return nil, err
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		return nil, err
	}

	log.Info("powering off server", "name", st.Name)
	action, _, err := client.Server.Poweroff(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("powering off server: %w", err)
	}
	if err := client.Action.WaitFor(ctx, action); err != nil {
		return nil, fmt.Errorf("waiting for power off: %w", err)
	}

	st.Status = "down"
	st.UpdatedAt = time.Now()
	return st, nil
}

func (h *hetznerProvider) Delete(ctx context.Context, st *state.BoxState) error {
	log := logging.Get()

	client, err := getClient()
	if err != nil {
		return err
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		return err
	}

	log.Info("deleting server", "name", st.Name)
	result, _, err := client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}

	// Wait for the delete action to complete.
	if result.Action != nil {
		log.Info("waiting for server deletion", "name", st.Name)
		if err := client.Action.WaitFor(ctx, result.Action); err != nil {
			return fmt.Errorf("waiting for server deletion: %w", err)
		}
	}

	// Poll until the server is fully gone.
	serverID := server.ID
	for {
		s, _, err := client.Server.GetByID(ctx, serverID)
		if err != nil {
			// API errors during cleanup are expected; treat as gone.
			break
		}
		if s == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	// Clean up the SSH key.
	if raw, ok := st.ProviderData["ssh_key_id"]; ok {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			sshKey, _, err := client.SSHKey.GetByID(ctx, id)
			if err == nil && sshKey != nil {
				_, _ = client.SSHKey.Delete(ctx, sshKey)
			}
		}
	}

	return nil
}

func (h *hetznerProvider) Status(ctx context.Context, st *state.BoxState) (*state.BoxState, error) {
	client, err := getClient()
	if err != nil {
		return nil, err
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		return nil, err
	}

	switch server.Status {
	case hcloud.ServerStatusRunning:
		st.Status = "up"
	case hcloud.ServerStatusOff:
		st.Status = "down"
	default:
		st.Status = "unknown"
	}

	if server.PublicNet.IPv4.IP != nil {
		st.IP = server.PublicNet.IPv4.IP.String()
	}

	st.UpdatedAt = time.Now()
	return st, nil
}
