package azure_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/provider/azure"
	"github.com/plombardi89/codebox/internal/state"
)

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()

	reg := provider.NewRegistry()
	reg.Register("azure", azure.New(slog.New(slog.DiscardHandler)))

	return reg
}

func TestAzureRegistered(t *testing.T) {
	reg := newTestRegistry(t)

	p, err := reg.Get("azure")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "azure", err)
	}

	if p == nil {
		t.Fatal("Get(\"azure\") returned nil provider")
	}
}

func TestAzureUpMissingSubscription(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")

	reg := newTestRegistry(t)

	p, err := reg.Get("azure")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "azure", err)
	}

	st := &state.Box{
		Name:     "test-box",
		Provider: "azure",
	}

	_, err = p.Up(context.Background(), st, "ssh-ed25519 AAAA...", nil)
	if err == nil {
		t.Fatal("Up() should have returned an error when AZURE_SUBSCRIPTION_ID is empty, got nil")
	}

	if !strings.Contains(err.Error(), "AZURE_SUBSCRIPTION_ID") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "AZURE_SUBSCRIPTION_ID")
	}
}
