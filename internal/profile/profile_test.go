package profile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/plombardi89/codebox/internal/profile"
)

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()

	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := []byte("packages:\n  - nodejs\n  - docker\n  - python3\n")
	if err := os.WriteFile(filepath.Join(profDir, "webdev.yaml"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, err := profile.Load(dir, "webdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"nodejs", "docker", "python3"}
	if len(p.Packages) != len(want) {
		t.Fatalf("got %d packages, want %d", len(p.Packages), len(want))
	}

	for i, pkg := range want {
		if p.Packages[i] != pkg {
			t.Errorf("Packages[%d] = %q, want %q", i, p.Packages[i], pkg)
		}
	}
}

func TestLoad_EmptyPackages(t *testing.T) {
	dir := t.TempDir()

	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := []byte("packages: []\n")
	if err := os.WriteFile(filepath.Join(profDir, "empty.yaml"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, err := profile.Load(dir, "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.Packages) != 0 {
		t.Errorf("got %d packages, want 0", len(p.Packages))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := profile.Load(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()

	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := []byte("packages: [not valid yaml!!!{{{")
	if err := os.WriteFile(filepath.Join(profDir, "bad.yaml"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := profile.Load(dir, "bad")
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoad_EmptyName(t *testing.T) {
	dir := t.TempDir()

	_, err := profile.Load(dir, "")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestDir(t *testing.T) {
	got := profile.Dir("/home/user/.codebox")

	want := filepath.Join("/home/user/.codebox", "profiles")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestPath(t *testing.T) {
	got := profile.Path("/home/user/.codebox", "webdev")

	want := filepath.Join("/home/user/.codebox", "profiles", "webdev.yaml")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
