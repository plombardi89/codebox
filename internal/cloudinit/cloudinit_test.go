package cloudinit_test

import (
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/cloudinit"
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

	if !strings.Contains(out, "fail2ban") {
		t.Error("output should contain fail2ban package")
	}

	if !strings.Contains(out, "firewalld") {
		t.Error("output should contain firewalld package")
	}

	if !strings.Contains(out, "Port 2222") {
		t.Error("output should contain Port 2222 in sshd config")
	}

	if !strings.Contains(out, "firewall-cmd --permanent --add-port=2222/tcp") {
		t.Error("output should contain firewall-cmd --permanent --add-port=2222/tcp")
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

func TestGenerate_HardeningConfig(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		TailScaleAuth: "",
	}

	out, err := cloudinit.Generate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fail2ban jail config content.
	if !strings.Contains(out, "[sshd]") {
		t.Error("output should contain [sshd] jail section")
	}
	if !strings.Contains(out, "enabled = true") {
		t.Error("output should contain enabled = true in fail2ban config")
	}
	if !strings.Contains(out, "port = 2222") {
		t.Error("output should contain port = 2222 in fail2ban config")
	}

	// Verify firewalld commands are present.
	if !strings.Contains(out, "systemctl enable --now firewalld") {
		t.Error("output should contain systemctl enable --now firewalld")
	}
	if !strings.Contains(out, "firewall-cmd --permanent --add-port=2222/tcp") {
		t.Error("output should contain firewall-cmd --permanent --add-port=2222/tcp")
	}
	if !strings.Contains(out, "firewall-cmd --permanent --remove-service=ssh") {
		t.Error("output should contain firewall-cmd --permanent --remove-service=ssh")
	}
	if !strings.Contains(out, "firewall-cmd --reload") {
		t.Error("output should contain firewall-cmd --reload")
	}
	if !strings.Contains(out, "systemctl enable --now fail2ban") {
		t.Error("output should contain systemctl enable --now fail2ban")
	}

	// Verify ordering: sshd restart before firewalld, firewalld before Go install.
	sshdIdx := strings.Index(out, "systemctl restart sshd")
	firewalldIdx := strings.Index(out, "systemctl enable --now firewalld")
	fail2banIdx := strings.Index(out, "systemctl enable --now fail2ban")
	goIdx := strings.Index(out, "go1.24.4")

	if sshdIdx < 0 || firewalldIdx < 0 || fail2banIdx < 0 || goIdx < 0 {
		t.Fatal("expected all hardening commands to be present")
	}

	if sshdIdx >= firewalldIdx {
		t.Error("sshd restart should come before firewalld enable")
	}
	if firewalldIdx >= fail2banIdx {
		t.Error("firewalld enable should come before fail2ban enable")
	}
	if fail2banIdx >= goIdx {
		t.Error("fail2ban enable should come before Go install")
	}
}
