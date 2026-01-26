# nbc

## What This Is

nbc is an alternative to `bootc` that allows users to install, manage, and upgrade Linux systems using OCI container images. It targets Debian/Ubuntu and Arch Linux — distros where upstream bootc support is immature. It's intended as a short-term solution until bootc gains broader distro support.

## Core Value

Users can reliably install and upgrade their Linux system from OCI images with A/B partitioning for atomic updates and rollback safety.

## Requirements

### Validated

- Install Linux system from OCI image to disk — existing
- A/B partition scheme with atomic updates — existing
- Upgrade to new OCI image version — existing
- LUKS encryption support — existing
- GRUB2 and systemd-boot bootloader support — existing
- OCI image pull from remote registries — existing
- Local container daemon support (podman/docker) — existing
- Staged install/update via local cache — existing
- JSON output mode for machine consumption — existing

### Active

- [ ] Reliable VM-based integration testing with Incus
- [ ] SDK/library exposure with clean API surface
- [ ] Progress reporting via `github.com/frostyard/pm/progress`
- [ ] Structured logging with slog to `/var/log/nbc/` or similar
- [ ] Consistent CLI interface (flags, commands, output formats)
- [ ] Clear, actionable error messages
- [ ] Predictable behavior in edge cases
- [ ] Documentation (help text, examples, usage)

### Out of Scope

- Fedora support — upstream bootc handles Fedora well
- Non-Linux platforms — Linux-only by design
- CGO dependencies — must remain pure Go
- GUI application — CLI and SDK only (consumers can build GUI)
- Container runtime dependency — OCI images pulled directly, no daemon required for remote images

## Context

**Current State:**
- Core install and upgrade functionality works
- Code is CLI-entangled — business logic mixed with command handling
- Output is scattered: `fmt.Print*`, existing progress abstraction, logging mixed with user output, direct `os.Stdout` writes
- Integration tests exist (Incus-based) but are flaky/non-deterministic
- Some unit tests exist but coverage is inconsistent

**Architecture (from codebase map):**
- Cobra/Viper CLI layer in `cmd/`
- Business logic in `pkg/` as composable structs
- External system interactions via `os/exec` (blkid, mount, cryptsetup, etc.)
- OCI handling via go-containerregistry (no daemon dependency)
- Existing `ProgressReporter` in `pkg/progress.go` (dual-mode text/JSON)

**Key Abstractions to Refactor:**
- `Installer` — orchestrates fresh installations
- `SystemUpdater` — handles A/B partition updates
- `ContainerExtractor` — extracts OCI images to filesystem
- `BootloaderInstaller` — installs/configures GRUB2 or systemd-boot
- `ImageCache` — manages OCI layout cache for staged operations

## Constraints

- **Pure Go**: No CGO dependencies — must build with `CGO_ENABLED=0`
- **Backward Compatibility**: Existing nbc-managed systems must continue to work after updates
- **Incus Compatibility**: Integration tests must work with existing Incus setup
- **Go Version**: Go 1.24+ required
- **Linux Only**: Target platform is Linux amd64 (arm64 for builds)

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Use `github.com/frostyard/pm/progress` for all output | Owned library, clean interface for SDK consumers | — Pending |
| Separate slog logging from user output | Debug logs to disk, progress to user — clean separation | — Pending |
| Testing before SDK extraction | Reliable tests enable safe refactoring | — Pending |
| SDK before UX polish | Clean API informs CLI design | — Pending |
| Full CLI parity for SDK | All CLI operations available programmatically | — Pending |

---
*Last updated: 2026-01-26 after initialization*
