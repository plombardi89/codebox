# codebox

CLI tool for managing remote development VMs on Hetzner Cloud or Azure. Spins up a Fedora VM with Go, OpenCode, zsh, and SSH key auth pre-configured via cloud-init.

## Prerequisites

- Go 1.24+ (to build)
- A Hetzner API token (`HCLOUD_TOKEN`) or Azure credentials (`AZURE_SUBSCRIPTION_ID` + `az login`)
- [mutagen](https://mutagen.io/) installed locally (only needed for `filesync`)
- [opencode](https://opencode.ai/) installed locally (only needed for `codebox opencode`)

## Build

```
make codebox
```

## Quick start

```sh
export HCLOUD_TOKEN=your-token

# Create a box and wait for it to be ready
codebox up mybox

# SSH in
codebox ssh mybox

# Or run OpenCode remotely and attach the local TUI
codebox opencode mybox

# Stop the VM (keeps state for later)
codebox down mybox
```

## Commands

### `codebox up <name>`

Create or start a codebox. If the box already exists, it resumes the existing VM.

```sh
codebox up mybox
codebox up mybox --provider azure --azure-vm-size standard_d4ads_v6
codebox up mybox --profile python
codebox up mybox --recreate              # destroy VM and recreate with fresh cloud-init
codebox up mybox --tailscale             # requires TAILSCALE_AUTHKEY env var
codebox up mybox --wait 10m              # wait up to 10m for SSH
codebox up mybox --wait 0               # skip waiting for SSH
```

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` | `hetzner` | Cloud provider (`hetzner` or `azure`) |
| `--hetzner-server-type` | `cx33` | Hetzner server type |
| `--hetzner-location` | `hel1` | Hetzner datacenter location |
| `--hetzner-image` | `fedora-43` | Hetzner OS image |
| `--azure-vm-size` | `standard_d2ads_v6` | Azure VM size |
| `--azure-location` | `canadacentral` | Azure region |
| `--azure-subscription-id` | | Azure subscription ID (overrides `AZURE_SUBSCRIPTION_ID`) |
| `--profile` | | Box profile name (see [Profiles](#profiles)) |
| `--recreate` | `false` | Delete and recreate the VM with fresh cloud-init |
| `--tailscale` | `false` | Enable TailScale (requires `TAILSCALE_AUTHKEY`) |
| `--wait` | `5m` | Wait for SSH after VM is ready (`0` to disable) |

### `codebox down <name>`

Stop a codebox. By default, only stops the VM — state and remote resources are preserved.

```sh
codebox down mybox                         # stop VM
codebox down mybox --delete                # stop and delete remote resources
codebox down mybox --delete --delete-local-storage  # also remove local state
```

| Flag | Default | Description |
|------|---------|-------------|
| `--delete` | `false` | Delete remote resources after stopping |
| `--delete-local-storage` | `false` | Remove local box directory after stopping |
| `--azure-subscription-id` | | Azure subscription ID override |

### `codebox ssh <name>`

SSH into a codebox.

```sh
codebox ssh mybox
codebox ssh mybox --wait                   # wait up to 5m for SSH to be ready
codebox ssh mybox --wait 10m               # wait up to 10m
codebox ssh mybox --manual                 # print the ssh command instead of running it
```

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | | Wait for SSH to become ready. Accepts an optional duration (default `5m` if no value given). |
| `--manual` | `false` | Print the ssh command instead of executing it |

### `codebox opencode <name>`

Attach to the OpenCode server running on a codebox.

The VM runs `opencode serve` as a systemd user service that starts automatically on boot and survives SSH disconnects. This command sets up an SSH tunnel with local port forwarding and runs `opencode attach` locally. When you exit the TUI, only the tunnel is torn down — the remote server keeps running.

```sh
codebox opencode mybox
codebox opencode mybox --wait              # wait for SSH first (useful right after 'up')
codebox opencode mybox --port 8080         # use a different local port
codebox opencode mybox --dir /home/dev/project  # set the working directory
codebox opencode mybox -s <session-id>     # reconnect to a specific session
```

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `4096` | Local port to forward to the remote OpenCode server |
| `--dir` | | Working directory for the OpenCode TUI on the remote box |
| `-s, --session` | | Session ID to continue |
| `--wait` | | Wait for SSH to become ready. Accepts an optional duration (default `5m`). |

### `codebox ls`

List all codeboxes and their status.

```sh
codebox ls
```

### `codebox filesync`

Manage bidirectional file sync sessions between local and remote paths using [mutagen](https://mutagen.io/).

```sh
# Start syncing local directories to the remote box
codebox filesync start mybox ./src:/home/dev/project/src ./config:/home/dev/project/config

# Check sync status
codebox filesync status mybox

# Pause / resume
codebox filesync pause mybox
codebox filesync resume mybox

# List all sync sessions for a box
codebox filesync ls mybox

# Stop all sync sessions
codebox filesync stop mybox
```

`filesync start` accepts `--mode` to set the mutagen sync mode (default: `two-way-safe`).

### `codebox sync`

Discover Azure codeboxes belonging to the current user and sync SSH keys and state locally. Useful when accessing boxes from a new machine.

```sh
codebox sync
codebox sync --azure-subscription-id your-sub-id
```

### Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--data-dir` | `~/.codebox` | Path to codebox data directory (or set `CODEBOX_DATA_DIR`) |
| `-v, --verbose` | `false` | Enable debug logging |

## Profiles

Profiles add extra OS packages to the cloud-init configuration. Create a YAML file at `~/.codebox/profiles/<name>.yaml`:

```yaml
# ~/.codebox/profiles/python.yaml
packages:
  - python3
  - python3-pip
  - python3-devel
```

Then use it when creating a box:

```sh
codebox up mybox --profile python
```

The profile is persisted in the box state. On `--recreate`, the same profile is reused unless you pass a different `--profile`.

## VM setup

VMs are provisioned with cloud-init on Fedora. The baseline configuration includes:

- **User:** `dev` with passwordless sudo and SSH key auth
- **Shell:** zsh with Oh My Zsh and the Aphrodite theme
- **Prompt:** `[codebox:<name>]` prepended to the zsh prompt
- **Go:** 1.24.4 installed to `/usr/local/go`
- **OpenCode:** installed to `~/.opencode/bin/opencode`, runs as a systemd user service (`opencode-serve.service`) on port 4096 with linger enabled
- **SSH:** runs on port 2222, root login disabled, password auth disabled
- **Firewall:** firewalld with only port 2222/tcp open
- **SELinux:** port 2222 allowed for sshd
- **fail2ban:** enabled, monitoring sshd on port 2222
- **systemd-binfmt:** masked (avoids rate-limit failures on cloud VMs)
- **TailScale:** optional, enabled with `--tailscale`

Profile packages are installed alongside the baseline packages.

## Common scenarios

### Spin up a box and start coding

```sh
codebox up work
codebox opencode work
```

`codebox up` waits for SSH by default (up to 5m), so the box is ready by the time it returns. Then `codebox opencode` tunnels to the remote OpenCode server and attaches the local TUI.

### Recreate a box with a different profile

```sh
codebox up work --recreate --profile nodejs
```

This destroys the existing VM and creates a fresh one with the new profile. Infrastructure (SSH keys, networks) is preserved.

### Sync local files to a remote box

```sh
codebox filesync start work ./my-project:/home/dev/my-project
codebox opencode work
```

Mutagen keeps the directories in sync bidirectionally. Changes made by OpenCode on the remote box are synced back locally.

### Access Azure boxes from a new machine

```sh
codebox sync --azure-subscription-id your-sub-id
codebox ls
codebox ssh work
```

`codebox sync` discovers existing boxes and downloads their SSH keys from Azure Key Vault.
