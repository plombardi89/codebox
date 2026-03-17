package azure_test

import (
	"context"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"

	// Import azure to trigger init() registration.
	_ "github.com/plombardi89/codebox/internal/provider/azure"
)

func TestAzureProviderName(t *testing.T) {
	p, err := provider.Get("azure")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "azure", err)
	}
	if name := p.Name(); name != "azure" {
		t.Errorf("Name() = %q, want %q", name, "azure")
	}
}

func TestAzureUpMissingSubscription(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")

	p, err := provider.Get("azure")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "azure", err)
	}

	st := &state.BoxState{
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

func TestAzureRegistered(t *testing.T) {
	p, err := provider.Get("azure")
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", "azure", err)
	}
	if p == nil {
		t.Fatal("Get(\"azure\") returned nil provider")
	}
}
