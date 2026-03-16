// Package cloudinit generates cloud-init user-data YAML for codebox VMs.
package cloudinit

import (
	"bytes"
	"errors"
	"text/template"

	"gopkg.in/yaml.v3"
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
package_update: true
package_upgrade: true

write_files:
  - path: /etc/ssh/sshd_config.d/99-codebox.conf
    content: |
      PermitRootLogin no
      PasswordAuthentication no
  - path: /etc/profile.d/golang.sh
    content: |
      export PATH="/usr/local/go/bin:$PATH"

runcmd:
  - systemctl restart sshd
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
	if cfg.SSHPubKey == "" {
		return "", errors.New("SSHPubKey must not be empty")
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
