package state_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/plombardi89/codebox/internal/state"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newTestState() *state.Box {
	return &state.Box{
		Name:     "testbox",
		Provider: "hetzner",
		Status:   "up",
		IP:       "1.2.3.4",
		SSHPort:  22,
		Image:    "fedora-43",
		ProviderData: map[string]string{
			"server_id": "12345",
			"region":    "eu-central",
		},
		CreatedAt: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt: time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC),
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	stateFile := state.Path(dir)

	original := newTestState()

	if err := state.Save(stateFile, original, discardLogger()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := state.Load(stateFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Name != original.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, original.Name)
	}

	if loaded.Provider != original.Provider {
		t.Errorf("Provider = %q, want %q", loaded.Provider, original.Provider)
	}

	if loaded.Status != original.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, original.Status)
	}

	if loaded.IP != original.IP {
		t.Errorf("IP = %q, want %q", loaded.IP, original.IP)
	}

	if loaded.SSHPort != original.SSHPort {
		t.Errorf("SSHPort = %d, want %d", loaded.SSHPort, original.SSHPort)
	}

	if loaded.Image != original.Image {
		t.Errorf("Image = %q, want %q", loaded.Image, original.Image)
	}

	if !loaded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", loaded.CreatedAt, original.CreatedAt)
	}

	if !loaded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", loaded.UpdatedAt, original.UpdatedAt)
	}

	if len(loaded.ProviderData) != len(original.ProviderData) {
		t.Fatalf("ProviderData length = %d, want %d", len(loaded.ProviderData), len(original.ProviderData))
	}

	for k, v := range original.ProviderData {
		if loaded.ProviderData[k] != v {
			t.Errorf("ProviderData[%q] = %q, want %q", k, loaded.ProviderData[k], v)
		}
	}
}

func TestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	stateFile := state.Path(dir)

	original := newTestState()

	if err := state.Save(stateFile, original, discardLogger()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the file exists.
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file does not exist after Save: %v", err)
	}

	// Verify the file contains valid JSON by loading it back.
	loaded, err := state.Load(stateFile)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}

	if loaded.Name != original.Name {
		t.Errorf("round-trip Name = %q, want %q", loaded.Name, original.Name)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "nonexistent", "state.json")

	_, err := state.Load(stateFile)
	if err == nil {
		t.Fatal("Load of missing file should return error")
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	stateFile := state.Path(dir)

	if err := os.WriteFile(stateFile, []byte("{not valid json!!!"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err := state.Load(stateFile)
	if err == nil {
		t.Fatal("Load of corrupt file should return error")
	}
}

func TestListAll(t *testing.T) {
	root := t.TempDir()

	// Create 3 subdirectories with state files.
	for _, name := range []string{"box1", "box2", "box3"} {
		boxDir := filepath.Join(root, name)
		if err := os.MkdirAll(boxDir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", name, err)
		}

		stateFile := state.Path(boxDir)
		if name == "box3" {
			// Write invalid JSON for box3.
			if err := os.WriteFile(stateFile, []byte("corrupt"), 0o644); err != nil {
				t.Fatalf("failed to write corrupt state for %s: %v", name, err)
			}

			continue
		}

		s := &state.Box{
			Name:     name,
			Provider: "azure",
			Status:   "up",
		}
		if err := state.Save(stateFile, s, discardLogger()); err != nil {
			t.Fatalf("Save failed for %s: %v", name, err)
		}
	}

	states, err := state.ListAll(root)
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	if len(states) != 2 {
		t.Fatalf("ListAll returned %d states, want 2", len(states))
	}
}

func TestListAll_EmptyDir(t *testing.T) {
	root := t.TempDir()

	states, err := state.ListAll(root)
	if err != nil {
		t.Fatalf("ListAll on empty dir failed: %v", err)
	}

	if len(states) != 0 {
		t.Fatalf("ListAll on empty dir returned %d states, want 0", len(states))
	}
}

func TestListAll_NonExistent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")

	states, err := state.ListAll(root)
	if err != nil {
		t.Fatalf("ListAll on non-existent dir failed: %v", err)
	}

	if len(states) != 0 {
		t.Fatalf("ListAll on non-existent dir returned %d states, want 0", len(states))
	}
}
