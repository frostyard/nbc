# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

nbc is a Go CLI tool for installing bootc-compatible container images to physical disks with A/B partitioning for atomic updates. It handles partitioning, filesystem creation, container extraction, and bootloader installation without requiring the `bootc` command.

## Build and Test Commands

```bash
# Build
make build                 # Build binary with version info

# Format and lint (ALWAYS run before committing)
make fmt                   # Format all Go code
make lint                  # Run golangci-lint

# Testing
make test-unit             # Unit tests only (no root required)
make test-integration      # Integration tests (requires root, auto-escalates with sudo)
make test                  # Run all tests (unit then integration)
make test-incus            # Full VM integration tests (requires root and Incus)

# Run specific test
go test -v ./pkg/... -run TestInstallConfig_Validate
```

**Never run `go test`, `go fmt`, or linter commands directly - always use Makefile targets.**

## Architecture

### Code Organization
- `cmd/` - Cobra CLI commands (parse flags, call pkg functions)
- `pkg/` - Core business logic (Installer, SystemUpdater, ContainerExtractor, etc.)
- `pkg/types/` - JSON output types for machine-readable output
- `pkg/testutil/` - Test helpers (disk simulation, mock containers)

### Key Components
- **Installer** (`pkg/install.go`) - Fresh installations to disk
- **SystemUpdater** (`pkg/update.go`) - A/B partition updates with rollback
- **ContainerExtractor** (`pkg/container.go`) - OCI image extraction via go-containerregistry
- **BootloaderInstaller** (`pkg/bootloader.go`) - GRUB2 or systemd-boot configuration
- **Reporter** (`pkg/reporter.go`) - Interface for all user-facing output (TextReporter, JSONReporter, NoopReporter)
- **Shared Steps** (`pkg/steps.go`) - Common install/update operations (SetupTargetSystem, ExtractAndVerifyContainer)
- **Workflow** (`pkg/workflow.go`) - Composable step-based orchestration type

### Data Flow
1. CLI parses flags (grouped in typed flag structs per command), creates config struct
2. `NewInstaller(cfg)` / `NewSystemUpdater()` validates and sets defaults
3. `Install(ctx)` / `Update()` orchestrates: partitioning → formatting → extraction → bootloader
4. All exported I/O functions accept `context.Context` for cancellation
5. System config stored in `/var/lib/nbc/state/config.json` (shared across A/B roots)

## Critical: Install and Update Parity

**Any change to installation flow MUST also be applied to update flow:**
- `pkg/install.go` - `Install()` function
- `pkg/update.go` - `Update()` function
- `pkg/steps.go` - Shared operations used by both (SetupTargetSystem, ExtractAndVerifyContainer)

Keep in sync: kernel cmdline parameters, tmpfiles.d configs, bootloader entries, directory creation.
Changes to shared steps in `pkg/steps.go` automatically apply to both flows.

## Code Conventions

### Error Handling
```go
if err != nil {
    return fmt.Errorf("failed to <action>: %w", err)
}
```
- Error strings should not end with punctuation or newlines
- Wrap errors with context using `%w`

### File Paths
- Always use `filepath.Join()` for path construction
- Never use string concatenation for paths

### Console Output
- Use the `Reporter` interface for all user-facing output — never raw `fmt.Print` in `pkg/`
- Implementations: `TextReporter` (human), `JSONReporter` (machine), `NoopReporter` (tests)
- Pass `Reporter` as a parameter to functions that produce output

### Dry-Run Pattern
```go
if dryRun {
    progress.Message("[DRY RUN] Would wipe disk: %s", device)
    return nil
}
```

## Process Locking

Operations use POSIX file locking to prevent concurrent access:
- Cache operations: `/var/run/nbc/cache.lock`
- System operations: `/var/run/nbc/system.lock`

```go
lock, err := AcquireSystemLock()
if err != nil {
    return err
}
defer func() { _ = lock.Release() }()
```

## Testing Patterns

### Test Naming
- Unit tests: `TestXxx`
- Integration tests: `TestIntegration_Xxx` (require root)
- Incus VM tests: `TestIncus_Xxx`

### Test Helpers
```go
testutil.RequireRoot(t)
testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat")
disk, err := testutil.CreateTestDisk(t, 50)  // 50GB sparse image
```

### Assertions
Use standard library only - no testify/gomega:
```go
if got != want {
    t.Errorf("result = %v, want %v", got, want)
}
```

## Secure Boot Chain (Important)

Shim is hardcoded to load `grubx64.efi` in the same directory. For systemd-boot with Secure Boot:
```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi.signed
├── grubx64.efi   ← systemd-bootx64.efi.signed (yes, named grubx64.efi)
└── mmx64.efi     ← MOK manager
```

**Do NOT include fbx64.efi** - it causes "Restore Boot Option" blue screen.

## Technical Details

### Partition Layout
1. EFI System Partition (FAT32) - UEFI boot files
2. Root1 (ext4/btrfs) - OS slot A
3. Root2 (ext4/btrfs) - OS slot B
4. Var (ext4/btrfs) - Shared `/var` partition

### Kernel Command Line Format
```
systemd.mount-extra=DEVICE:MOUNTPOINT:FSTYPE:OPTIONS
```
Example: `systemd.mount-extra=UUID=abc123:/var:ext4:defaults`

### Container Requirements
Kernel/initramfs must be in `/usr/lib/modules/$KERNEL_VERSION/`:
- `vmlinuz` or `vmlinuz-$KERNEL_VERSION`
- `initramfs.img` or `initrd.img`
