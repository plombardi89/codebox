# Project Overview

Codebox is a tool for starting a remote VM in Azure or Hetzner cloud that runs headless OpenCode.

## Repository Structure

Repo is organized into several directories:

- `bin/` - where generated binary artifacts should be placed.
- `cmd/` - where the sources for each binary artifact are located. Each subdirectory corresponds to a binary artifact.
    - `codebox` - The codebox binary
- `docs/` - documentation for the project.
- `hack/` - where development tools and scripts are located.
    - `cmd/` - development tools that are built as Go binaries.
    - `scratch/` - scratch space for quick go experiments.
- `internal/` - where shared but internal to this project packages are located.
- `tmp/` - project local temporary directory for intermediate stuff that will be cleaned up quickly.

## Building and Testing

- `codebox`:
    - build: `make codebox`

## Coding Standards

- Do not cross `cmd/` package boundaries. For example, `cmd/codebox` should not import from `cmd/codebox`.
  If you need to share code between these packages, put it in `internal/`.

## Testing Standards

- Add tests for new behavior. Cover success, failure, and edge cases.

## Boundaries

- **Ask first**
    - Large cross-package refactors.
    - New dependencies with broad impact.
    - Destructive data or migration changes.
- **Never**
    - Commit secrets, credentials, or tokens.
    - Edit generated files by hand when a generation workflow exists.
    - Use destructive git operations unless explicitly requested.
    - Go outside the project boundary, for example, DO NOT edit files in user's home directories, add or edit files
      in /tmp or anywhere else on the host filesystem.
