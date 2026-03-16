package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BoxState represents the persisted state of a box.
type BoxState struct {
	Name         string
	Provider     string
	Status       string // "up", "down", "unknown"
	IP           string
	SSHPort      int
	ProviderData map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// StatePath returns the path to the state file within a box directory.
func StatePath(boxDir string) string {
	return filepath.Join(boxDir, "state.json")
}

// Load reads and unmarshals a state file.
func Load(stateFile string) (*BoxState, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s BoxState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	return &s, nil
}

// Save marshals and writes the state file atomically using a temp file and rename.
func Save(stateFile string, state *BoxState) error {
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
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, stateFile); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// ListAll reads all subdirectories of dataDir, loads each state.json, and returns the list.
// Directories without a valid state.json are skipped with a warning to stderr.
func ListAll(dataDir string) ([]*BoxState, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading data directory: %w", err)
	}

	var states []*BoxState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		boxDir := filepath.Join(dataDir, entry.Name())
		stateFile := StatePath(boxDir)

		s, err := Load(stateFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", entry.Name(), err)
			continue
		}

		states = append(states, s)
	}

	return states, nil
}
