package datadir

import (
	"os"
	"path/filepath"
)

// BoxDir returns the path to a named box directory.
func BoxDir(dataDir, name string) string {
	return filepath.Join(dataDir, name)
}

// SSHDir returns the path to a box's ssh directory.
func SSHDir(dataDir, name string) string {
	return filepath.Join(dataDir, name, "ssh")
}

// EnsureBoxDir creates the box directory tree (including ssh/) with mode 0700.
func EnsureBoxDir(dataDir, name string) error {
	sshDir := SSHDir(dataDir, name)
	return os.MkdirAll(sshDir, 0o700)
}

// RemoveBoxDir removes the named box directory entirely.
func RemoveBoxDir(dataDir, name string) error {
	return os.RemoveAll(BoxDir(dataDir, name))
}
