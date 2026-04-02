# Codebox Design Document

- Single Go binary (`bin/codebox`).

# High-Level

A codebox is VM running in a cloud such as Azure or Hetzner.

# Initial Requirements

- Codebox can start a VM locally using Podman Machines OR remotely running in Azure or Hetzner Cloud.
- Codebox stores its configuration and state in `$HOME/.codebox` or if `CODEBOX_DATA_DIR` is set then it
  uses that directory instead. From here on we will refer to the data directory as `CODEBOX_DATA_DIR`.
- Each codebox manages should have its own data directory `$CODEBOX_DATA_DIR/<name>`.
- Each codebox should have its own unique SSH key-pair. The SSH key-pair should in `$CODEBOX_DATA_DIR/ssh/` and have the
  name `id_<type>` for the private key and `id_<type>.pub` for the public key.
- There should be a command `codebox ssh <name>` which connects the user to the codebox ssh server. The `--manual` flag
  should instead of connecting the user automatically just print the necessary `ssh ...` command they have to use.
- There should be a command `codebox ls` which lists all of user's codebox instances and if they are up/down. The
  `ls` command should also show the provider, box IP and Port for SSH.
- There should be a command `codebox down` which shuts down a remote codebox. Optionally if `--delete` is provided then
  the codebox is deleted.
- There should be a command `codebox up` which starts a codebox and if it does not exist then it creates the codebox.
- Because `codebox up` needs to support multiple providers any provider specific configuration should be a flag prefix.
  For example `--$provider-some-option`

# v2 Features

- cloud-init script for Fedora operating systems
  - Ensure OS is up to date
  - Add a non-root user "dev". The dev user should have password-less sudo.
  - Disable root SSH login.
  - Install ZSH and ensure it is the default shell.
  - Install OpenCode
  - Install TailScale.
  - Install Go

# v3 Features

- Server hardening. Need to install:
  - fail2ban and firewall
  - Run SSH on port 2222 instead of 22. Configure firewalld to let the new port in only.
- Implement logging (slog) so that when user's run codebox they can see what it is doing. Logging should be turned on
  with CODEBOX_LOGGING=<level> and should be hidden from stdout by default.
- Support for Azure virtual machines.

# v4 Features

- A codebox user might work from multiple computers. Need a way to sync the state of available CodeBox instances to
  their current machine.
  - Not supported for Hetzner
  - For Azure
    - Put SSH private key in key vault that exists for each codebox.
    - Store basic configuration on the Resource Group tags for the Codebox.
    - Find all Codeboxes for a User by listing codebox resource groups and discriminating by the user's Object ID.