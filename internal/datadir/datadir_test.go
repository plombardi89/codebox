package datadir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/plombardi89/codebox/internal/datadir"
)

func TestBoxDir(t *testing.T) {
	got := datadir.BoxDir("root", "mybox")

	want := filepath.Join("root", "mybox")
	if got != want {
		t.Errorf("BoxDir(\"root\", \"mybox\") = %q, want %q", got, want)
	}
}

func TestSSHDir(t *testing.T) {
	got := datadir.SSHDir("root", "mybox")

	want := filepath.Join("root", "mybox", "ssh")
	if got != want {
		t.Errorf("SSHDir(\"root\", \"mybox\") = %q, want %q", got, want)
	}
}

func TestEnsureBoxDir(t *testing.T) {
	root := t.TempDir()

	if err := datadir.EnsureBoxDir(root, "testbox"); err != nil {
		t.Fatalf("EnsureBoxDir failed: %v", err)
	}

	// Verify BoxDir exists and is a directory with mode 0700.
	boxPath := datadir.BoxDir(root, "testbox")

	fi, err := os.Stat(boxPath)
	if err != nil {
		t.Fatalf("BoxDir does not exist: %v", err)
	}

	if !fi.IsDir() {
		t.Fatal("BoxDir is not a directory")
	}

	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("BoxDir permissions = %o, want 0700", perm)
	}

	// Verify SSHDir exists and is a directory with mode 0700.
	sshPath := datadir.SSHDir(root, "testbox")

	fi, err = os.Stat(sshPath)
	if err != nil {
		t.Fatalf("SSHDir does not exist: %v", err)
	}

	if !fi.IsDir() {
		t.Fatal("SSHDir is not a directory")
	}

	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("SSHDir permissions = %o, want 0700", perm)
	}
}

func TestEnsureBoxDir_Idempotent(t *testing.T) {
	root := t.TempDir()

	if err := datadir.EnsureBoxDir(root, "testbox"); err != nil {
		t.Fatalf("first EnsureBoxDir failed: %v", err)
	}

	if err := datadir.EnsureBoxDir(root, "testbox"); err != nil {
		t.Fatalf("second EnsureBoxDir failed: %v", err)
	}
}

func TestRemoveBoxDir(t *testing.T) {
	root := t.TempDir()

	if err := datadir.EnsureBoxDir(root, "testbox"); err != nil {
		t.Fatalf("EnsureBoxDir failed: %v", err)
	}

	if err := datadir.RemoveBoxDir(root, "testbox"); err != nil {
		t.Fatalf("RemoveBoxDir failed: %v", err)
	}

	boxPath := datadir.BoxDir(root, "testbox")
	if _, err := os.Stat(boxPath); !os.IsNotExist(err) {
		t.Fatalf("BoxDir still exists after removal (err=%v)", err)
	}
}

func TestRemoveBoxDir_NonExistent(t *testing.T) {
	root := t.TempDir()

	if err := datadir.RemoveBoxDir(root, "doesnotexist"); err != nil {
		t.Fatalf("RemoveBoxDir on non-existent dir returned error: %v", err)
	}
}
