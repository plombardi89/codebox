package sshconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/sshconfig"
)

func TestHostAlias(t *testing.T) {
	got := sshconfig.HostAlias("mybox")
	want := "codebox-mybox"

	if got != want {
		t.Errorf("HostAlias() = %q, want %q", got, want)
	}
}

func TestConfigPath(t *testing.T) {
	got := sshconfig.ConfigPath("/home/user/.codebox")
	want := filepath.Join("/home/user/.codebox", "ssh_config")

	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestWriteBoxEntry_NewFile(t *testing.T) {
	dataDir := t.TempDir()

	// Create the box SSH directory so the key path is well-formed.
	boxSSHDir := filepath.Join(dataDir, "mybox", "ssh")
	if err := os.MkdirAll(boxSSHDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sshconfig.WriteBoxEntry(dataDir, "mybox", "1.2.3.4", 2222); err != nil {
		t.Fatalf("WriteBoxEntry: %v", err)
	}

	content, err := os.ReadFile(sshconfig.ConfigPath(dataDir))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	s := string(content)

	for _, want := range []string{
		"Host codebox-mybox",
		"HostName 1.2.3.4",
		"Port 2222",
		"User dev",
		"IdentityFile",
		"StrictHostKeyChecking no",
		"UserKnownHostsFile /dev/null",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config should contain %q, got:\n%s", want, s)
		}
	}
}

func TestWriteBoxEntry_UpdateExisting(t *testing.T) {
	dataDir := t.TempDir()

	boxSSHDir := filepath.Join(dataDir, "mybox", "ssh")
	if err := os.MkdirAll(boxSSHDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write initial entry.
	if err := sshconfig.WriteBoxEntry(dataDir, "mybox", "1.2.3.4", 2222); err != nil {
		t.Fatalf("WriteBoxEntry (initial): %v", err)
	}

	// Update with new IP.
	if err := sshconfig.WriteBoxEntry(dataDir, "mybox", "5.6.7.8", 2222); err != nil {
		t.Fatalf("WriteBoxEntry (update): %v", err)
	}

	content, err := os.ReadFile(sshconfig.ConfigPath(dataDir))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	s := string(content)

	if strings.Contains(s, "1.2.3.4") {
		t.Error("old IP should be replaced")
	}

	if !strings.Contains(s, "5.6.7.8") {
		t.Error("new IP should be present")
	}

	// Should only have one Host block.
	if strings.Count(s, "Host codebox-mybox") != 1 {
		t.Error("should only have one Host block")
	}
}

func TestWriteBoxEntry_MultipleBoxes(t *testing.T) {
	dataDir := t.TempDir()

	for _, name := range []string{"box1", "box2"} {
		boxSSHDir := filepath.Join(dataDir, name, "ssh")
		if err := os.MkdirAll(boxSSHDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if err := sshconfig.WriteBoxEntry(dataDir, "box1", "1.1.1.1", 2222); err != nil {
		t.Fatalf("WriteBoxEntry box1: %v", err)
	}

	if err := sshconfig.WriteBoxEntry(dataDir, "box2", "2.2.2.2", 2222); err != nil {
		t.Fatalf("WriteBoxEntry box2: %v", err)
	}

	content, err := os.ReadFile(sshconfig.ConfigPath(dataDir))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	s := string(content)

	if !strings.Contains(s, "Host codebox-box1") {
		t.Error("should contain box1 entry")
	}

	if !strings.Contains(s, "Host codebox-box2") {
		t.Error("should contain box2 entry")
	}

	if !strings.Contains(s, "1.1.1.1") {
		t.Error("should contain box1 IP")
	}

	if !strings.Contains(s, "2.2.2.2") {
		t.Error("should contain box2 IP")
	}
}

func TestRemoveBoxEntry(t *testing.T) {
	dataDir := t.TempDir()

	for _, name := range []string{"box1", "box2"} {
		boxSSHDir := filepath.Join(dataDir, name, "ssh")
		if err := os.MkdirAll(boxSSHDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if err := sshconfig.WriteBoxEntry(dataDir, "box1", "1.1.1.1", 2222); err != nil {
		t.Fatalf("WriteBoxEntry box1: %v", err)
	}

	if err := sshconfig.WriteBoxEntry(dataDir, "box2", "2.2.2.2", 2222); err != nil {
		t.Fatalf("WriteBoxEntry box2: %v", err)
	}

	if err := sshconfig.RemoveBoxEntry(dataDir, "box1"); err != nil {
		t.Fatalf("RemoveBoxEntry: %v", err)
	}

	content, err := os.ReadFile(sshconfig.ConfigPath(dataDir))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	s := string(content)

	if strings.Contains(s, "Host codebox-box1") {
		t.Error("box1 entry should be removed")
	}

	if !strings.Contains(s, "Host codebox-box2") {
		t.Error("box2 entry should still be present")
	}
}

func TestRemoveBoxEntry_NoFile(t *testing.T) {
	dataDir := t.TempDir()

	// Should not error when the file doesn't exist.
	if err := sshconfig.RemoveBoxEntry(dataDir, "nonexistent"); err != nil {
		t.Fatalf("RemoveBoxEntry on missing file: %v", err)
	}
}

func TestEnsureInclude_CreatesFile(t *testing.T) {
	// Use a fake home directory so we don't touch the real ~/.ssh/config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dataDir := filepath.Join(fakeHome, ".codebox")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sshconfig.EnsureInclude(dataDir); err != nil {
		t.Fatalf("EnsureInclude: %v", err)
	}

	sshConfigPath := filepath.Join(fakeHome, ".ssh", "config")

	content, err := os.ReadFile(sshConfigPath)
	if err != nil {
		t.Fatalf("reading ssh config: %v", err)
	}

	expected := "Include " + sshconfig.ConfigPath(dataDir)
	if !strings.Contains(string(content), expected) {
		t.Errorf("ssh config should contain %q, got:\n%s", expected, string(content))
	}
}

func TestEnsureInclude_Idempotent(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dataDir := filepath.Join(fakeHome, ".codebox")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Call twice.
	if err := sshconfig.EnsureInclude(dataDir); err != nil {
		t.Fatalf("EnsureInclude (1): %v", err)
	}

	if err := sshconfig.EnsureInclude(dataDir); err != nil {
		t.Fatalf("EnsureInclude (2): %v", err)
	}

	sshConfigPath := filepath.Join(fakeHome, ".ssh", "config")

	content, err := os.ReadFile(sshConfigPath)
	if err != nil {
		t.Fatalf("reading ssh config: %v", err)
	}

	expected := "Include " + sshconfig.ConfigPath(dataDir)
	count := strings.Count(string(content), expected)

	if count != 1 {
		t.Errorf("Include line should appear exactly once, found %d times in:\n%s", count, string(content))
	}
}

func TestEnsureInclude_PreservesExistingContent(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	sshDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existingContent := "Host myserver\n    HostName 10.0.0.1\n    User admin\n"
	sshConfigPath := filepath.Join(sshDir, "config")

	if err := os.WriteFile(sshConfigPath, []byte(existingContent), 0o600); err != nil {
		t.Fatalf("writing existing ssh config: %v", err)
	}

	dataDir := filepath.Join(fakeHome, ".codebox")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sshconfig.EnsureInclude(dataDir); err != nil {
		t.Fatalf("EnsureInclude: %v", err)
	}

	content, err := os.ReadFile(sshConfigPath)
	if err != nil {
		t.Fatalf("reading ssh config: %v", err)
	}

	s := string(content)

	// Include should be at the top.
	expected := "Include " + sshconfig.ConfigPath(dataDir)
	if !strings.HasPrefix(s, expected) {
		t.Errorf("Include should be at the top of the file, got:\n%s", s)
	}

	// Existing content should still be present.
	if !strings.Contains(s, "Host myserver") {
		t.Error("existing SSH config content should be preserved")
	}
}
