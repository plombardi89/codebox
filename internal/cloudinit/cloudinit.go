// Package cloudinit generates cloud-init user-data YAML for codebox VMs.
package cloudinit

import (
	"bytes"
	"errors"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/plombardi89/codebox/internal/logging"
)

// Config holds the parameters for generating cloud-init user-data.
type Config struct {
	SSHPubKey     string // public key in authorized_keys format
	TailScaleAuth string // TailScale auth key (empty = skip TailScale setup)
}

const cloudInitTemplate = `#cloud-config
disable_root: true
ssh_pwauth: false

users:
  - name: dev
    shell: /bin/bash
    groups: wheel
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    lock_passwd: true
    ssh_authorized_keys:
      - "{{.SSHPubKey}}"

packages:
  - zsh
  - git
  - curl
  - tar
  - fail2ban
  - firewalld
package_update: true
package_upgrade: true

write_files:
  - path: /etc/fail2ban/jail.d/codebox.conf
    content: |
      [sshd]
      enabled = true
      port = 2222
  - path: /etc/ssh/sshd_config.d/99-codebox.conf
    content: |
      Port 2222
      PermitRootLogin no
      PasswordAuthentication no
  - path: /etc/profile.d/golang.sh
    content: |
      export PATH="/usr/local/go/bin:$PATH"

runcmd:
  - systemctl restart sshd
  - systemctl enable --now firewalld
  - firewall-cmd --permanent --add-port=2222/tcp
  - firewall-cmd --permanent --remove-service=ssh
  - firewall-cmd --reload
  - systemctl enable --now fail2ban
  - curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
  - su - dev -c 'curl -fsSL https://opencode.ai/install | bash'
  - chsh -s /usr/bin/zsh dev
{{- if .TailScaleAuth}}
  - curl -fsSL https://tailscale.com/install.sh | sh
  - tailscale up --authkey={{.TailScaleAuth}}
{{- end}}
`

var tmpl = template.Must(template.New("cloud-init").Parse(cloudInitTemplate))

// Generate renders cloud-init user-data YAML from the given config.
// It returns an error if SSHPubKey is empty or the rendered output is not valid YAML.
func Generate(cfg Config) (string, error) {
	log := logging.Get()

	if cfg.SSHPubKey == "" {
		return "", errors.New("SSHPubKey must not be empty")
	}

	log.Debug("generating cloud-init")
	if cfg.TailScaleAuth != "" {
		log.Debug("tailscale enabled")
	} else {
		log.Debug("tailscale disabled")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", err
	}

	out := buf.String()

	// Validate the rendered YAML (skip the #cloud-config directive line).
	var check any
	if err := yaml.Unmarshal([]byte(out), &check); err != nil {
		return "", err
	}

	return out, nil
}
