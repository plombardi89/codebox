# codebox

CLI tool for managing remote development VMs on Hetzner Cloud. Spins up a Fedora VM with Go, OpenCode, and SSH key auth pre-configured via cloud-init.

## Build

```
make codebox
```

## Usage

Set your Hetzner API token:

```
export HCLOUD_TOKEN=your-token
```

Create a VM:

```
bin/codebox up mybox
```

SSH in:

```
bin/codebox ssh mybox
```

List boxes:

```
bin/codebox ls
```

Stop a VM:

```
bin/codebox down mybox
```

Stop and destroy remote resources:

```
bin/codebox down mybox --delete
```

Stop, destroy remote resources, and remove local state:

```
bin/codebox down mybox --delete --delete-local-storage
```

## Flags

`codebox up` accepts:

- `--provider` - cloud provider (default: `hetzner`)
- `--hetzner-server-type` - server type (default: `cx33`)
- `--hetzner-location` - datacenter (default: `hel1`)
- `--hetzner-image` - OS image (default: `fedora-43`)
- `--tailscale` - enable TailScale (requires `TAILSCALE_AUTHKEY` env var)
- `--data-dir` - data directory (default: `$CODEBOX_DATA_DIR` or `~/.codebox`)

## VM setup

VMs are provisioned with cloud-init:

- User `dev` with sudo, SSH key auth
- Go 1.24.4
- OpenCode
- Root login and password auth disabled
- Optional TailScale
