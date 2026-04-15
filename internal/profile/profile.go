// Package profile loads box profiles that specify additional packages to
// install via cloud-init when creating a new codebox.
package profile

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Profile describes additional packages to install on a codebox.
type Profile struct {
	// Packages lists OS package names to install alongside the baseline set.
	Packages []string `yaml:"packages"`
}

// Dir returns the path to the profiles directory within the data directory.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, "profiles")
}

// Path returns the path to a named profile YAML file.
func Path(dataDir, name string) string {
	return filepath.Join(Dir(dataDir), name+".yaml")
}

// Load reads and parses a named profile from the data directory.
// The file is expected at <dataDir>/profiles/<name>.yaml.
func Load(dataDir, name string) (*Profile, error) {
	if name == "" {
		return nil, fmt.Errorf("profile name must not be empty")
	}

	path := Path(dataDir, name)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile %q: %w", name, err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile %q: %w", name, err)
	}

	return &p, nil
}
