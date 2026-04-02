// Package sshkey generates and manages per-codebox ed25519 SSH key pairs.
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	privateKeyFile = "id_ed25519"
	publicKeyFile  = "id_ed25519.pub"
)

// Generate creates an ed25519 key pair and writes the private key
// (mode 0600) and public key (mode 0644) into sshDir. If both key
// files already exist, it returns nil without overwriting them.
func Generate(sshDir string, log *slog.Logger) error {
	privPath := PrivateKeyPath(sshDir)
	pubPath := PublicKeyPath(sshDir)

	// If both keys already exist, reuse them.
	privExists, err := fileExists(privPath)
	if err != nil {
		return fmt.Errorf("sshkey: stat %s: %w", privPath, err)
	}

	pubExists, err := fileExists(pubPath)
	if err != nil {
		return fmt.Errorf("sshkey: stat %s: %w", pubPath, err)
	}

	if privExists && pubExists {
		log.Debug("keys already exist, skipping generation", "dir", sshDir)
		return nil
	}

	log.Info("generating ed25519 key pair", "dir", sshDir)

	// Generate the key pair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("sshkey: generate key: %w", err)
	}

	// Marshal the private key to OpenSSH PEM format.
	privPEM, err := ssh.MarshalPrivateKey(priv, "" /* no passphrase comment */)
	if err != nil {
		return fmt.Errorf("sshkey: marshal private key: %w", err)
	}

	privBytes := pem.EncodeToMemory(privPEM)

	// Marshal the public key to OpenSSH authorized_keys format.
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return fmt.Errorf("sshkey: marshal public key: %w", err)
	}

	pubBytes := ssh.MarshalAuthorizedKey(sshPub)

	return WriteKeyPair(sshDir, privBytes, pubBytes)
}

// PrivateKeyPath returns the path to the private key inside sshDir.
func PrivateKeyPath(sshDir string) string {
	return filepath.Join(sshDir, privateKeyFile)
}

// PublicKeyPath returns the path to the public key inside sshDir.
func PublicKeyPath(sshDir string) string {
	return filepath.Join(sshDir, publicKeyFile)
}

// ReadPublicKey reads and returns the public key file content as a
// string with trailing whitespace removed.
func ReadPublicKey(sshDir string) (string, error) {
	data, err := os.ReadFile(PublicKeyPath(sshDir))
	if err != nil {
		return "", fmt.Errorf("sshkey: read public key: %w", err)
	}

	return strings.TrimRight(string(data), " \t\r\n"), nil
}

// DerivePublicKey parses an OpenSSH PEM private key and returns the
// corresponding public key in authorized_keys format.
func DerivePublicKey(privateKeyPEM []byte) ([]byte, error) {
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("sshkey: parse private key: %w", err)
	}

	return ssh.MarshalAuthorizedKey(signer.PublicKey()), nil
}

// WriteKeyPair writes an SSH key pair into sshDir, creating the directory
// (mode 0700) if needed. The private key is written with mode 0600 and the
// public key with mode 0644.
func WriteKeyPair(sshDir string, privateKeyPEM, publicKey []byte) error {
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("sshkey: create directory %s: %w", sshDir, err)
	}

	if err := os.WriteFile(PrivateKeyPath(sshDir), privateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("sshkey: write private key: %w", err)
	}

	if err := os.WriteFile(PublicKeyPath(sshDir), publicKey, 0o644); err != nil {
		return fmt.Errorf("sshkey: write public key: %w", err)
	}

	return nil
}

// fileExists returns true if the path exists, false if it does not,
// or an error for any other stat failure.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}
