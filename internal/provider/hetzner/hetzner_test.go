package hetzner_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/provider/hetzner"
	"github.com/plombardi89/codebox/internal/state"
)

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()

	reg := provider.NewRegistry()
	reg.Register("hetzner", hetzner.New(slog.New(slog.DiscardHandler)))

	return reg
}

func TestHetznerRegistered(t *testing.T) {
	reg := newTestRegistry(t)

	p, err := reg.Get("hetzner")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "hetzner", err)
	}

	if p == nil {
		t.Fatal("Get(\"hetzner\") returned nil provider")
	}
}

func TestHetznerUnknownProvider(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("Get(\"nonexistent\") should have returned an error, got nil")
	}

	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "nonexistent")
	}
}

func TestHetznerUpMissingToken(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	reg := newTestRegistry(t)

	p, err := reg.Get("hetzner")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "hetzner", err)
	}

	st := &state.Box{
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
