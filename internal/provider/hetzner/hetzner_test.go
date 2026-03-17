package hetzner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"

	// Import hetzner to trigger init() registration.
	_ "github.com/plombardi89/codebox/internal/provider/hetzner"
)

func TestHetznerRegistered(t *testing.T) {
	p, err := provider.Get("hetzner")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "hetzner", err)
	}
	if p == nil {
		t.Fatal("Get(\"hetzner\") returned nil provider")
	}
	if name := p.Name(); name != "hetzner" {
		t.Errorf("Name() = %q, want %q", name, "hetzner")
	}
}

func TestHetznerUnknownProvider(t *testing.T) {
	_, err := provider.Get("nonexistent")
	if err == nil {
		t.Fatal("Get(\"nonexistent\") should have returned an error, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "nonexistent")
	}
}

func TestHetznerUpMissingToken(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	p, err := provider.Get("hetzner")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "hetzner", err)
	}

	st := &state.BoxState{
		Name:     "test-box",
		Provider: "hetzner",
	}

	_, err = p.Up(context.Background(), st, "ssh-ed25519 AAAA...", nil)
	if err == nil {
		t.Fatal("Up() should have returned an error when HCLOUD_TOKEN is empty, got nil")
	}
	if !strings.Contains(err.Error(), "HCLOUD_TOKEN") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "HCLOUD_TOKEN")
	}
}
