package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Status constants for Box.
const (
	StatusUp      = "up"
	StatusDown    = "down"
	StatusUnknown = "unknown"
)

// Well-known defaults shared across providers and CLI.
const (
	DefaultSSHPort = 2222
	DefaultUser    = "dev"
)

// Box represents the persisted state of a box.
type Box struct {
	Name         string
	Provider     string
	Status       string // StatusUp, StatusDown, StatusUnknown
	IP           string
	SSHPort      int
	Image        string // OS image (e.g. "fedora-43", "Fedora-Cloud-43-x64")
	ProviderData map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EnsureProviderData initialises the ProviderData map if it is nil.
func (s *Box) EnsureProviderData() {
	if s.ProviderData == nil {
		s.ProviderData = make(map[string]string)
	}
}

// SetUp marks the box as running and records its network details.
func (s *Box) SetUp(ip string) {
	s.Status = StatusUp
	s.IP = ip
	s.SSHPort = DefaultSSHPort
	s.UpdatedAt = time.Now()
}

// SetDown marks the box as stopped.
func (s *Box) SetDown() {
	s.Status = StatusDown
	s.UpdatedAt = time.Now()
}

// Path returns the path to the state file within a box directory.
func Path(boxDir string) string {
	return filepath.Join(boxDir, "state.json")
}

// Load reads and unmarshals a state file.
func Load(stateFile string) (*Box, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s Box
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	return &s, nil
}

// Save marshals and writes the state file atomically using a temp file and rename.
func Save(stateFile string, state *Box, log *slog.Logger) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(stateFile)

	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			log.Debug("closing temp file during cleanup", "error", closeErr)
		}

		if removeErr := os.Remove(tmpName); removeErr != nil {
			log.Debug("removing temp file during cleanup", "error", removeErr)
		}

		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		if removeErr := os.Remove(tmpName); removeErr != nil {
			log.Debug("removing temp file during cleanup", "error", removeErr)
		}

		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, stateFile); err != nil {
		if removeErr := os.Remove(tmpName); removeErr != nil {
			log.Debug("removing temp file during cleanup", "error", removeErr)
		}

		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// ListAll reads all subdirectories of dataDir, loads each state.json, and returns the list.
// Directories without a valid state.json are skipped with a warning to stderr.
func ListAll(dataDir string) ([]*Box, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading data directory: %w", err)
	}

	var states []*Box

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		boxDir := filepath.Join(dataDir, entry.Name())
		stateFile := Path(boxDir)

		s, err := Load(stateFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", entry.Name(), err)
			continue
		}

		states = append(states, s)
	}

	return states, nil
}
