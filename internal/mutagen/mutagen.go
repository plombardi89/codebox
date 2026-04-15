// Package mutagen wraps the mutagen CLI to manage file-sync sessions between
// a local machine and a remote codebox.
package mutagen

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// LabelManaged is applied to every session so we can enumerate codebox sessions.
const LabelManaged = "codebox=true"

// LabelBoxKey is the label key used to tag sessions with their box name.
const LabelBoxKey = "codebox-box"

// DefaultSyncMode is the default mutagen sync mode.
const DefaultSyncMode = "two-way-safe"

// EnsureInstalled checks that the mutagen binary is available in PATH.
func EnsureInstalled() error {
	_, err := exec.LookPath("mutagen")
	if err != nil {
		return fmt.Errorf("mutagen is not installed or not in PATH; install it from https://mutagen.io")
	}

	return nil
}

// SessionName returns a deterministic session name for a box + path pair.
// Format: codebox-<boxName>-<hash8> where hash8 is derived from the paths.
func SessionName(boxName, localPath, remotePath string) string {
	h := sha256.Sum256([]byte(localPath + "\x00" + remotePath))

	return fmt.Sprintf("codebox-%s-%x", boxName, h[:4])
}

// BoxLabelSelector returns a mutagen label selector that matches all sessions
// for the given box name.
func BoxLabelSelector(boxName string) string {
	return fmt.Sprintf("%s=%s", LabelBoxKey, boxName)
}

// ParsePathPair splits a "<local>:<remote>" argument into its two components.
// The split is performed on the last colon to allow Windows-style paths like
// C:\Users\... on the local side.
func ParsePathPair(arg string) (local, remote string, err error) {
	idx := strings.LastIndex(arg, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid path pair %q: expected <local>:<remote>", arg)
	}

	local = arg[:idx]
	remote = arg[idx+1:]

	if local == "" {
		return "", "", fmt.Errorf("invalid path pair %q: local path is empty", arg)
	}

	if remote == "" {
		return "", "", fmt.Errorf("invalid path pair %q: remote path is empty", arg)
	}

	return local, remote, nil
}

// RemoteEndpoint formats a mutagen remote endpoint using an SSH host alias.
// The alias must be configured in the SSH config (see sshconfig package)
// so that ssh knows the hostname, port, user, and identity file.
func RemoteEndpoint(hostAlias, path string) string {
	return hostAlias + ":" + path
}

// CreateOpts holds the parameters for creating a sync session.
type CreateOpts struct {
	BoxName    string
	LocalPath  string
	RemotePath string
	HostAlias  string // SSH config host alias (e.g. "codebox-mybox")
	SyncMode   string // e.g. "two-way-safe"
}

// CreateSession creates a new mutagen sync session.
func CreateSession(ctx context.Context, opts CreateOpts, log *slog.Logger) error {
	name := SessionName(opts.BoxName, opts.LocalPath, opts.RemotePath)

	mode := opts.SyncMode
	if mode == "" {
		mode = DefaultSyncMode
	}

	remoteEndpoint := RemoteEndpoint(opts.HostAlias, opts.RemotePath)

	args := []string{
		"sync", "create",
		"--name", name,
		"--sync-mode", mode,
		"--label", LabelManaged,
		"--label", fmt.Sprintf("%s=%s", LabelBoxKey, opts.BoxName),
		opts.LocalPath,
		remoteEndpoint,
	}

	log.Debug("creating mutagen session", "name", name, "args", args)

	return run(ctx, args, log)
}

// StopSessions terminates all mutagen sync sessions for a box.
func StopSessions(ctx context.Context, boxName string, log *slog.Logger) error {
	args := []string{
		"sync", "terminate",
		"--label-selector", BoxLabelSelector(boxName),
	}

	log.Debug("terminating mutagen sessions", "box", boxName)

	return run(ctx, args, log)
}

// PauseSessions pauses all mutagen sync sessions for a box.
func PauseSessions(ctx context.Context, boxName string, log *slog.Logger) error {
	args := []string{
		"sync", "pause",
		"--label-selector", BoxLabelSelector(boxName),
	}

	log.Debug("pausing mutagen sessions", "box", boxName)

	return run(ctx, args, log)
}

// ResumeSessions resumes all mutagen sync sessions for a box.
func ResumeSessions(ctx context.Context, boxName string, log *slog.Logger) error {
	args := []string{
		"sync", "resume",
		"--label-selector", BoxLabelSelector(boxName),
	}

	log.Debug("resuming mutagen sessions", "box", boxName)

	return run(ctx, args, log)
}

// ListSessions lists mutagen sync sessions. If boxName is non-empty, only
// sessions for that box are shown. Returns the raw mutagen output.
func ListSessions(ctx context.Context, boxName string, log *slog.Logger) (string, error) {
	args := []string{"sync", "list"}
	if boxName != "" {
		args = append(args, "--label-selector", BoxLabelSelector(boxName))
	} else {
		args = append(args, "--label-selector", LabelManaged)
	}

	log.Debug("listing mutagen sessions", "box", boxName)

	return runOutput(ctx, args, log)
}

// StatusSession shows the status of sync sessions for a box.
func StatusSession(ctx context.Context, boxName string, log *slog.Logger) (string, error) {
	args := []string{
		"sync", "list",
		"--label-selector", BoxLabelSelector(boxName),
	}

	log.Debug("getting mutagen session status", "box", boxName)

	return runOutput(ctx, args, log)
}

// run executes a mutagen command.
func run(ctx context.Context, args []string, log *slog.Logger) error {
	cmd := exec.CommandContext(ctx, "mutagen", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mutagen %s: %w", args[0]+" "+args[1], err)
	}

	return nil
}

// runOutput executes a mutagen command and captures its stdout.
func runOutput(ctx context.Context, args []string, log *slog.Logger) (string, error) {
	cmd := exec.CommandContext(ctx, "mutagen", args...)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mutagen %s: %w", args[0]+" "+args[1], err)
	}

	return string(out), nil
}
