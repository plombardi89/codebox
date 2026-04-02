package hetzner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"
)

// errServerNotFound is returned by getServer when the Hetzner API returns nil
// for the requested server ID.
var errServerNotFound = errors.New("server not found")

// Provider data keys persisted in Box.ProviderData.
const (
	KeyServerID = "server_id"
	KeySSHKeyID = "ssh_key_id"
)

// New returns a new Hetzner provider that logs to log.
func New(log *slog.Logger) provider.Provider {
	return &hetznerProvider{log: log}
}

// hetznerProvider implements provider.Provider using the Hetzner Cloud API.
type hetznerProvider struct {
	log *slog.Logger
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
func getServer(ctx context.Context, client *hcloud.Client, st *state.Box) (*hcloud.Server, error) {
	raw, ok := st.ProviderData[KeyServerID]
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
		return nil, fmt.Errorf("server %d: %w", id, errServerNotFound)
	}

	return server, nil
}

func (h *hetznerProvider) Up(ctx context.Context, st *state.Box, pubKey string, opts map[string]string) (*state.Box, error) {
	client, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("creating hcloud client: %w", err)
	}

	serverType := provider.OptOrDefault(opts, "server-type", "cx33")
	location := provider.OptOrDefault(opts, "location", "hel1")
	image := provider.OptOrDefault(opts, "image", "fedora-43")

	// If a server already exists, handle idempotently.
	if _, ok := st.ProviderData[KeyServerID]; ok {
		h.log.Debug("server ID found in state", "server_id", st.ProviderData[KeyServerID])

		server, err := getServer(ctx, client, st)
		if err != nil {
			return nil, fmt.Errorf("fetching existing server: %w", err)
		}

		switch server.Status {
		case hcloud.ServerStatusRunning:
			h.log.Debug("server already running", "name", st.Name)
			st.SetUp(server.PublicNet.IPv4.IP.String())
			h.log.Debug("server IP", "ip", st.IP)

			return st, nil

		case hcloud.ServerStatusOff:
			h.log.Info("powering on server", "name", st.Name)

			action, _, err := client.Server.Poweron(ctx, server)
			if err != nil {
				return nil, fmt.Errorf("powering on server: %w", err)
			}

			if err := client.Action.WaitFor(ctx, action); err != nil {
				return nil, fmt.Errorf("waiting for power on: %w", err)
			}

			st.SetUp(server.PublicNet.IPv4.IP.String())
			h.log.Debug("server IP", "ip", st.IP)

			return st, nil

		default:
			return nil, fmt.Errorf("server is in unexpected status: %s", server.Status)
		}
	}

	// No existing server -- create from scratch.
	st.EnsureProviderData()

	// Create or reuse SSH key in Hetzner.
	sshKeyName := fmt.Sprintf("codebox-%s", st.Name)

	sshKey, _, err := client.SSHKey.GetByName(ctx, sshKeyName)
	if err != nil {
		return nil, fmt.Errorf("looking up SSH key: %w", err)
	}

	if sshKey == nil {
		h.log.Info("creating SSH key", "name", sshKeyName)

		sshKey, _, err = client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
			Name:      sshKeyName,
			PublicKey: pubKey,
		})
		if err != nil {
			return nil, fmt.Errorf("creating SSH key: %w", err)
		}
	} else {
		h.log.Debug("SSH key already exists", "name", sshKeyName)
	}

	st.ProviderData[KeySSHKeyID] = fmt.Sprintf("%d", sshKey.ID)

	// Create server.
	h.log.Info("creating server", "name", st.Name, "type", serverType, "location", location, "image", image)

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

	st.ProviderData[KeyServerID] = fmt.Sprintf("%d", result.Server.ID)
	st.SetUp(result.Server.PublicNet.IPv4.IP.String())
	st.Image = image
	h.log.Debug("server created", "server_id", result.Server.ID, "ip", st.IP)

	return st, nil
}

func (h *hetznerProvider) Down(ctx context.Context, st *state.Box) (*state.Box, error) {
	client, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("creating hcloud client: %w", err)
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		if errors.Is(err, errServerNotFound) {
			h.log.Info("server already gone, skipping power off", "name", st.Name)
			st.SetDown()

			return st, nil
		}

		return nil, fmt.Errorf("fetching server for power off: %w", err)
	}

	h.log.Info("powering off server", "name", st.Name)

	action, _, err := client.Server.Poweroff(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("powering off server: %w", err)
	}

	if err := client.Action.WaitFor(ctx, action); err != nil {
		return nil, fmt.Errorf("waiting for power off: %w", err)
	}

	st.SetDown()

	return st, nil
}

func (h *hetznerProvider) DestroyVM(ctx context.Context, st *state.Box) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("creating hcloud client: %w", err)
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		if errors.Is(err, errServerNotFound) {
			h.log.Info("server already gone, skipping deletion", "name", st.Name)
			return nil
		}

		return fmt.Errorf("fetching server for deletion: %w", err)
	}

	h.log.Info("deleting server", "name", st.Name)

	result, _, err := client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}

	if result.Action != nil {
		if err := client.Action.WaitFor(ctx, result.Action); err != nil {
			return fmt.Errorf("waiting for server deletion: %w", err)
		}
	}

	// Clear server ID so Up() creates a new one.
	delete(st.ProviderData, KeyServerID)

	return nil
}

func (h *hetznerProvider) Delete(ctx context.Context, st *state.Box) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("creating hcloud client: %w", err)
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		if errors.Is(err, errServerNotFound) {
			h.log.Info("server already gone, skipping deletion", "name", st.Name)
		} else {
			return fmt.Errorf("fetching server for deletion: %w", err)
		}
	} else {
		h.log.Info("deleting server", "name", st.Name)

		result, _, err := client.Server.DeleteWithResult(ctx, server)
		if err != nil {
			return fmt.Errorf("deleting server: %w", err)
		}

		// Wait for the delete action to complete.
		if result.Action != nil {
			h.log.Info("waiting for server deletion", "name", st.Name)

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
	}

	// Clean up the SSH key.
	if raw, ok := st.ProviderData[KeySSHKeyID]; ok {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			h.log.Warn("could not parse SSH key ID for cleanup", "ssh_key_id", raw, "error", err)
		} else {
			sshKey, _, err := client.SSHKey.GetByID(ctx, id)
			if err != nil {
				h.log.Warn("could not fetch SSH key for cleanup", "ssh_key_id", id, "error", err)
			} else if sshKey != nil {
				if _, err := client.SSHKey.Delete(ctx, sshKey); err != nil {
					h.log.Warn("could not delete SSH key", "ssh_key_id", id, "error", err)
				}
			}
		}
	}

	return nil
}

func (h *hetznerProvider) Status(ctx context.Context, st *state.Box) (*state.Box, error) {
	client, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("creating hcloud client: %w", err)
	}

	server, err := getServer(ctx, client, st)
	if err != nil {
		return nil, fmt.Errorf("fetching server status: %w", err)
	}

	switch server.Status {
	case hcloud.ServerStatusRunning:
		st.Status = state.StatusUp
	case hcloud.ServerStatusOff:
		st.Status = state.StatusDown
	default:
		st.Status = state.StatusUnknown
	}

	if server.PublicNet.IPv4.IP != nil {
		st.IP = server.PublicNet.IPv4.IP.String()
	}

	st.UpdatedAt = time.Now()

	return st, nil
}
