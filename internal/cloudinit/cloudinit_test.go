package cloudinit_test

import (
	"strings"
	"testing"

	"github.com/voidfunktion/ocbox/internal/cloudinit"
	"gopkg.in/yaml.v3"
)

func TestGenerate_Basic(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		TailScaleAuth: "",
	}

	out, err := cloudinit.Generate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Errorf("output should start with #cloud-config, got: %q", out[:40])
	}

	if !strings.Contains(out, "dev") {
		t.Error("output should contain user 'dev'")
	}

	if !strings.Contains(out, "go1.24.4") {
		t.Error("output should contain Go version go1.24.4")
	}

	if !strings.Contains(out, "opencode.ai/install") {
		t.Error("output should contain opencode.ai/install")
	}

	if !strings.Contains(out, "PasswordAuthentication no") {
		t.Error("output should contain PasswordAuthentication no")
	}

	if strings.Contains(strings.ToLower(out), "tailscale") {
		t.Error("output should NOT contain tailscale when TailScaleAuth is empty")
	}

	// Validate YAML
	var parsed any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("output is not valid YAML: %v", err)
	}
}

func TestGenerate_WithTailScale(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		TailScaleAuth: "tskey-auth-abc123",
	}

	out, err := cloudinit.Generate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "tailscale up --authkey=tskey-auth-abc123") {
		t.Error("output should contain tailscale up --authkey=tskey-auth-abc123")
	}

	if !strings.Contains(out, "tailscale.com/install.sh") {
		t.Error("output should contain tailscale.com/install.sh")
	}
}

func TestGenerate_EmptyPubKey(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "",
		TailScaleAuth: "",
	}

	_, err := cloudinit.Generate(cfg)
	if err == nil {
		t.Fatal("expected an error for empty SSHPubKey, got nil")
	}

	if !strings.Contains(err.Error(), "SSHPubKey") {
		t.Errorf("error message should contain 'SSHPubKey', got: %v", err)
	}
}
