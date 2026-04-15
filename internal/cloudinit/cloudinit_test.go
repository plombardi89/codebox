package cloudinit_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/cloudinit"
	"gopkg.in/yaml.v3"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestGenerate_Basic(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		TailScaleAuth: "",
	}

	out, err := cloudinit.Generate(cfg, discardLogger())
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

	if !strings.Contains(out, "ohmyzsh/ohmyzsh") {
		t.Error("output should contain oh-my-zsh install")
	}

	if !strings.Contains(out, "aphrodite.zsh-theme") {
		t.Error("output should contain aphrodite theme install")
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

	if !strings.Contains(out, "policycoreutils-python-utils") {
		t.Error("output should contain policycoreutils-python-utils package")
	}

	if !strings.Contains(out, "semanage port -a -t ssh_port_t -p tcp 2222") {
		t.Error("output should contain semanage command to allow sshd on port 2222")
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

	out, err := cloudinit.Generate(cfg, discardLogger())
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

	_, err := cloudinit.Generate(cfg, discardLogger())
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

	out, err := cloudinit.Generate(cfg, discardLogger())
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

	// Verify ordering: firewalld before semanage, semanage before sshd config write,
	// sshd config write before sshd restart, fail2ban after sshd restart, Go install after fail2ban.
	firewalldIdx := strings.Index(out, "systemctl enable --now firewalld")
	semanageIdx := strings.Index(out, "semanage port -a -t ssh_port_t -p tcp 2222")
	sshdCfgIdx := strings.Index(out, "printf 'Port 2222")
	sshdIdx := strings.Index(out, "systemctl restart sshd")
	fail2banIdx := strings.Index(out, "systemctl enable --now fail2ban")
	goIdx := strings.Index(out, "go1.24.4")

	if firewalldIdx < 0 || semanageIdx < 0 || sshdCfgIdx < 0 || sshdIdx < 0 || fail2banIdx < 0 || goIdx < 0 {
		t.Fatal("expected all hardening commands to be present")
	}

	if firewalldIdx >= semanageIdx {
		t.Error("firewalld enable should come before semanage")
	}

	if semanageIdx >= sshdCfgIdx {
		t.Error("semanage should come before sshd config write")
	}

	if sshdCfgIdx >= sshdIdx {
		t.Error("sshd config write should come before sshd restart")
	}

	if sshdIdx >= fail2banIdx {
		t.Error("sshd restart should come before fail2ban enable")
	}

	if fail2banIdx >= goIdx {
		t.Error("fail2ban enable should come before Go install")
	}
}

func TestGenerate_WithExtraPackages(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		ExtraPackages: []string{"nodejs", "docker", "python3"},
	}

	out, err := cloudinit.Generate(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pkg := range cfg.ExtraPackages {
		if !strings.Contains(out, "  - "+pkg) {
			t.Errorf("output should contain package %q", pkg)
		}
	}

	// Baseline packages must still be present.
	for _, pkg := range []string{"zsh", "git", "curl", "tar", "fail2ban", "firewalld"} {
		if !strings.Contains(out, "  - "+pkg) {
			t.Errorf("output should still contain baseline package %q", pkg)
		}
	}

	// Validate YAML.
	var parsed any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("output is not valid YAML: %v", err)
	}
}

func TestGenerate_EmptyExtraPackages(t *testing.T) {
	cfgWithout := cloudinit.Config{
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
	}

	cfgWith := cloudinit.Config{
		SSHPubKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		ExtraPackages: []string{},
	}

	outWithout, err := cloudinit.Generate(cfgWithout, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outWith, err := cloudinit.Generate(cfgWith, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outWithout != outWith {
		t.Error("empty ExtraPackages should produce the same output as nil ExtraPackages")
	}
}

func TestGenerate_WithBoxName(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
		BoxName:   "mybox",
	}

	out, err := cloudinit.Generate(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// runcmd should create the prompt file via printf.
	if !strings.Contains(out, `> ~/.codebox-prompt.zsh`) {
		t.Error("output should contain runcmd creating .codebox-prompt.zsh")
	}

	// The prompt content should include the box name.
	if !strings.Contains(out, `[codebox:mybox]`) {
		t.Error("output should contain PROMPT with box name")
	}

	// runcmd should source the prompt file.
	if !strings.Contains(out, `source ~/.codebox-prompt.zsh`) {
		t.Error("output should contain runcmd to source .codebox-prompt.zsh")
	}

	// No write_files entry for the prompt file (must be runcmd only).
	writeFilesIdx := strings.Index(out, "write_files:")
	runcmdIdx := strings.Index(out, "runcmd:")
	promptFileIdx := strings.Index(out, ".codebox-prompt.zsh")

	if promptFileIdx < runcmdIdx {
		t.Error("prompt file creation should be in runcmd, not write_files")
	}

	// The printf and source lines must come AFTER Oh My Zsh theme sed and BEFORE chsh.
	sourceIdx := strings.Index(out, "source ~/.codebox-prompt.zsh")
	themeIdx := strings.Index(out, "ZSH_THEME")
	chshIdx := strings.Index(out, "chsh -s /usr/bin/zsh dev")

	if sourceIdx < 0 || themeIdx < 0 || chshIdx < 0 || writeFilesIdx < 0 {
		t.Fatal("expected all prompt-related commands to be present")
	}

	if themeIdx >= sourceIdx {
		t.Error("theme sed should come before prompt source")
	}

	if sourceIdx >= chshIdx {
		t.Error("prompt source should come before chsh")
	}

	// Validate YAML.
	var parsed any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("output is not valid YAML: %v", err)
	}
}

func TestGenerate_WithoutBoxName(t *testing.T) {
	cfg := cloudinit.Config{
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... test@host",
	}

	out, err := cloudinit.Generate(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No prompt customization when BoxName is empty.
	if strings.Contains(out, ".codebox-prompt.zsh") {
		t.Error("output should NOT contain .codebox-prompt.zsh when BoxName is empty")
	}

	if strings.Contains(out, "source ~/.codebox-prompt") {
		t.Error("output should NOT contain prompt source runcmd when BoxName is empty")
	}
}
