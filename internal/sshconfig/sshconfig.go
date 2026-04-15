// Package sshconfig manages an SSH config file for codebox instances so that
// tools like mutagen (which shell out to the system ssh) can reach codeboxes
// using a host alias without needing custom SSH flags.
package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
)

// HostAlias returns the SSH config host alias for a box name.
func HostAlias(name string) string {
	return "codebox-" + name
}

// ConfigPath returns the path to the codebox SSH config file.
func ConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "ssh_config")
}

// WriteBoxEntry writes or updates a Host entry for the given box in the
// codebox SSH config file at <dataDir>/ssh_config.
func WriteBoxEntry(dataDir, name, ip string, port int) error {
	configPath := ConfigPath(dataDir)
	alias := HostAlias(name)

	keyPath := sshkey.PrivateKeyPath(filepath.Join(dataDir, name, "ssh"))

	entry := formatEntry(alias, ip, port, state.DefaultUser, keyPath)

	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading SSH config: %w", err)
	}

	var updated string
	if len(existing) > 0 {
		updated = replaceOrAppendEntry(string(existing), alias, entry)
	} else {
		updated = entry
	}

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing SSH config: %w", err)
	}

	return nil
}

// RemoveBoxEntry removes the Host entry for a box from the codebox SSH config
// file. If the file does not exist or the entry is not found, this is a no-op.
func RemoveBoxEntry(dataDir, name string) error {
	configPath := ConfigPath(dataDir)
	alias := HostAlias(name)

	existing, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("reading SSH config: %w", err)
	}

	updated := removeEntry(string(existing), alias)

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing SSH config: %w", err)
	}

	return nil
}

// EnsureInclude ensures that ~/.ssh/config contains an Include directive for
// the codebox SSH config file. The Include is prepended at the top of the file
// because OpenSSH requires Include directives before any Host blocks.
// Creates ~/.ssh/ and ~/.ssh/config if they do not exist.
func EnsureInclude(dataDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determining home directory: %w", err)
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", sshDir, err)
	}

	sshConfigPath := filepath.Join(sshDir, "config")
	configPath := ConfigPath(dataDir)
	includeLine := "Include " + configPath

	existing, err := os.ReadFile(sshConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", sshConfigPath, err)
	}

	// Check if the Include is already present.
	if len(existing) > 0 {
		scanner := bufio.NewScanner(strings.NewReader(string(existing)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == includeLine {
				return nil // already included
			}
		}
	}

	// Prepend the Include directive.
	var content string
	if len(existing) > 0 {
		content = includeLine + "\n\n" + string(existing)
	} else {
		content = includeLine + "\n"
	}

	if err := os.WriteFile(sshConfigPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", sshConfigPath, err)
	}

	return nil
}

// formatEntry renders a single Host block for the SSH config.
func formatEntry(alias, ip string, port int, user, keyPath string) string {
	return fmt.Sprintf("Host %s\n    HostName %s\n    Port %d\n    User %s\n    IdentityFile %s\n    StrictHostKeyChecking no\n    UserKnownHostsFile /dev/null\n",
		alias, ip, port, user, keyPath)
}

// hostBlockPattern matches a "Host <alias>" line.
var hostBlockPattern = regexp.MustCompile(`(?m)^Host\s+`)

// replaceOrAppendEntry replaces an existing Host block for the given alias,
// or appends a new one if not found. A Host block spans from its "Host" line
// to the next "Host" line or end of file.
func replaceOrAppendEntry(content, alias, newEntry string) string {
	start, end, found := findEntryBounds(content, alias)
	if !found {
		// Append with a blank line separator if content doesn't end with one.
		sep := "\n"
		if strings.HasSuffix(content, "\n\n") || content == "" {
			sep = ""
		} else if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		}

		return content + sep + newEntry
	}

	return content[:start] + newEntry + content[end:]
}

// removeEntry removes the Host block for the given alias from the content.
func removeEntry(content, alias string) string {
	start, end, found := findEntryBounds(content, alias)
	if !found {
		return content
	}

	result := content[:start] + content[end:]

	// Clean up double blank lines left behind.
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n")

	return result
}

// findEntryBounds locates the start and end byte offsets of a Host block
// for the given alias within the SSH config content.
func findEntryBounds(content, alias string) (start, end int, found bool) {
	lines := strings.Split(content, "\n")
	hostLine := "Host " + alias

	inBlock := false
	startLine := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == hostLine {
			inBlock = true
			startLine = i

			continue
		}

		if inBlock && hostBlockPattern.MatchString(trimmed) {
			// Found the next Host block — our block ends here.
			start = offsetOfLine(lines, startLine)
			end = offsetOfLine(lines, i)

			return start, end, true
		}
	}

	if inBlock {
		// Block extends to end of file.
		start = offsetOfLine(lines, startLine)

		return start, len(content), true
	}

	return 0, 0, false
}

// offsetOfLine returns the byte offset of the given line index in
// a newline-split slice.
func offsetOfLine(lines []string, idx int) int {
	offset := 0
	for i := 0; i < idx; i++ {
		offset += len(lines[i]) + 1 // +1 for the \n
	}

	return offset
}
