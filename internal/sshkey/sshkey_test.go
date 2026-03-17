package sshkey_test

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()

	if err := sshkey.Generate(dir); err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	// Verify private key file exists with mode 0600.
	privPath := filepath.Join(dir, "id_ed25519")
	privInfo, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key file not found: %v", err)
	}
	if mode := privInfo.Mode().Perm(); mode != 0o600 {
		t.Errorf("private key mode = %04o, want 0600", mode)
	}

	// Verify public key file exists with mode 0644.
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("public key file not found: %v", err)
	}
	if mode := pubInfo.Mode().Perm(); mode != 0o644 {
		t.Errorf("public key mode = %04o, want 0644", mode)
	}

	// Parse the public key and verify type.
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("reading public key: %v", err)
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey(pubData)
	if err != nil {
		t.Fatalf("ParseAuthorizedKey() returned error: %v", err)
	}
	if keyType := parsed.Type(); keyType != "ssh-ed25519" {
		t.Errorf("public key type = %q, want %q", keyType, "ssh-ed25519")
	}

	// Parse the private key and verify it's ed25519.
	privData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("reading private key: %v", err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(privData)
	if err != nil {
		t.Fatalf("ParseRawPrivateKey() returned error: %v", err)
	}
	if _, ok := rawKey.(*ed25519.PrivateKey); !ok {
		t.Errorf("private key type = %T, want *ed25519.PrivateKey", rawKey)
	}
}

func TestGenerate_IdempotentWhenKeysExist(t *testing.T) {
	dir := t.TempDir()

	if err := sshkey.Generate(dir); err != nil {
		t.Fatalf("first Generate() returned error: %v", err)
	}

	// Read the original keys to verify they are not overwritten.
	origPriv, err := os.ReadFile(sshkey.PrivateKeyPath(dir))
	if err != nil {
		t.Fatalf("reading private key: %v", err)
	}
	origPub, err := os.ReadFile(sshkey.PublicKeyPath(dir))
	if err != nil {
		t.Fatalf("reading public key: %v", err)
	}

	// Second call should succeed without error.
	if err := sshkey.Generate(dir); err != nil {
		t.Fatalf("second Generate() returned error: %v", err)
	}

	// Keys should be unchanged.
	newPriv, err := os.ReadFile(sshkey.PrivateKeyPath(dir))
	if err != nil {
		t.Fatalf("reading private key after second Generate: %v", err)
	}
	newPub, err := os.ReadFile(sshkey.PublicKeyPath(dir))
	if err != nil {
		t.Fatalf("reading public key after second Generate: %v", err)
	}
	if string(origPriv) != string(newPriv) {
		t.Error("private key was overwritten by second Generate()")
	}
	if string(origPub) != string(newPub) {
		t.Error("public key was overwritten by second Generate()")
	}
}

func TestReadPublicKey(t *testing.T) {
	dir := t.TempDir()

	if err := sshkey.Generate(dir); err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	pub, err := sshkey.ReadPublicKey(dir)
	if err != nil {
		t.Fatalf("ReadPublicKey() returned error: %v", err)
	}

	if pub == "" {
		t.Fatal("ReadPublicKey() returned empty string")
	}
	if !strings.HasPrefix(pub, "ssh-ed25519") {
		t.Errorf("ReadPublicKey() = %q, want prefix %q", pub, "ssh-ed25519")
	}
}

func TestPrivateKeyPath(t *testing.T) {
	dir := "/some/dir"
	got := sshkey.PrivateKeyPath(dir)
	want := filepath.Join(dir, "id_ed25519")
	if got != want {
		t.Errorf("PrivateKeyPath(%q) = %q, want %q", dir, got, want)
	}
}

func TestPublicKeyPath(t *testing.T) {
	dir := "/some/dir"
	got := sshkey.PublicKeyPath(dir)
	want := filepath.Join(dir, "id_ed25519.pub")
	if got != want {
		t.Errorf("PublicKeyPath(%q) = %q, want %q", dir, got, want)
	}
}
