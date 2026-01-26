# Architecture

**Analysis Date:** 2026-01-26

## Pattern Overview

**Overall:** Layered CLI Architecture

nbc is a Go CLI application for installing bootc-compatible container images to physical disks with A/B partitioning for atomic updates.

**Key Characteristics:**
- Cobra/Viper-based CLI layer in `cmd/` for command parsing and user interaction
- Business logic in `pkg/` as composable structs with constructor-style initialization
- External system interactions via `os/exec` calls to system tools (blkid, mount, cryptsetup, etc.)
- OCI image handling via go-containerregistry library (no Docker/Podman daemon dependency)
- Dual-mode output: human-readable text or JSON Lines for machine consumption

## Layers

**CLI Layer (`cmd/`):**
- Purpose: Parse commands, flags, handle user interaction, orchestrate pkg functions
- Location: `cmd/`
- Contains: Cobra command definitions, flag binding, interactive prompts
- Depends on: `pkg/` for all business logic
- Used by: `main.go` entry point

**Business Logic Layer (`pkg/`):**
- Purpose: Core installation, update, and system management operations
- Location: `pkg/`
- Contains: Installer, SystemUpdater, ContainerExtractor, BootloaderInstaller, ImageCache, ProgressReporter
- Depends on: System tools via exec, go-containerregistry for OCI operations
- Used by: `cmd/` layer

**Type Definitions (`pkg/types/`):**
- Purpose: JSON output structures for machine-readable output
- Location: `pkg/types/types.go`
- Contains: StatusOutput, ListOutput, ProgressEvent, LintResult, etc.
- Depends on: Nothing (pure data types)
- Used by: `pkg/` for JSON serialization, external consumers

**Test Utilities (`pkg/testutil/`):**
- Purpose: Shared test fixtures and helpers
- Location: `pkg/testutil/`
- Contains: Disk simulation helpers
- Used by: `pkg/*_test.go` files

## Data Flow

**Installation Flow:**

1. CLI parses flags and creates `InstallConfig` struct
2. `NewInstaller(cfg)` validates config, applies defaults
3. `installer.SetCallbacks()` configures progress reporting
4. `installer.Install(ctx)` orchestrates:
   - Device setup (loopback or physical disk)
   - System lock acquisition
   - Partition creation (`CreatePartitions`)
   - Optional LUKS encryption (`SetupLUKS`)
   - Filesystem formatting (`FormatPartitions`)
   - Mount (`MountPartitions`)
   - Container extraction (`ContainerExtractor.Extract`)
   - System configuration (fstab, machine-id, /etc overlay)
   - Bootloader installation (`BootloaderInstaller.Install`)
5. Result returned with cleanup function for resource release

**Update Flow:**

1. CLI creates `SystemUpdater` with device and image ref
2. `PrepareUpdate()` detects existing partition scheme and active/inactive slots
3. `IsUpdateNeeded()` compares local vs remote image digests
4. `Update()` orchestrates:
   - Mount inactive root partition
   - Clear old content
   - Extract new container filesystem
   - Merge /etc configuration
   - Update bootloader to point to new root
5. Reboot activates new system (user-initiated)

**State Management:**
- System config stored in `/var/lib/nbc/state/config.json`
- Contains: image ref, digest, device, kernel args, bootloader type, encryption config
- Shared across A/B roots via shared `/var` partition
- Read by `ReadSystemConfig()`, written by `WriteSystemConfig()`

## Key Abstractions

**Installer (`pkg/install.go`):**
- Purpose: Orchestrates fresh installations to disk
- Entry: `NewInstaller(cfg *InstallConfig) (*Installer, error)`
- Pattern: Builder-style with `SetCallbacks()`, then `Install(ctx)`
- Key types: `InstallConfig`, `InstallCallbacks`, `InstallResult`

**SystemUpdater (`pkg/update.go`):**
- Purpose: Handles A/B partition updates with rollback support
- Entry: `NewSystemUpdater(device, imageRef string) *SystemUpdater`
- Pattern: Configure with setters, then `PerformUpdate(skipPull bool)`
- Determines active slot, updates inactive, switches bootloader

**ContainerExtractor (`pkg/container.go`):**
- Purpose: Extracts OCI container images to filesystem
- Entry: `NewContainerExtractor(imageRef, targetDir string)`
- Sources: Remote registry, local daemon (podman/docker), OCI layout directory
- Handles: Layer extraction, whiteouts, symlinks, permissions (SUID/SGID)

**BootloaderInstaller (`pkg/bootloader.go`):**
- Purpose: Installs and configures GRUB2 or systemd-boot
- Entry: `NewBootloaderInstaller(targetDir, device, scheme, osName)`
- Types: `BootloaderGRUB2`, `BootloaderSystemdBoot`
- Handles: Kernel cmdline generation, LUKS parameters, Secure Boot chain with shim

**ImageCache (`pkg/cache.go`):**
- Purpose: Manages OCI layout cache for offline/staged operations
- Entry: `NewImageCache(cacheDir)`, `NewStagedInstallCache()`, `NewStagedUpdateCache()`
- Locations: `/var/cache/nbc/staged-install/`, `/var/cache/nbc/staged-update/`
- Pattern: Download once, install/update from local cache

**ProgressReporter (`pkg/progress.go`):**
- Purpose: Dual-mode output (text or JSON Lines)
- Entry: `NewProgressReporter(jsonOutput bool, totalSteps int)`
- Methods: `Step()`, `Message()`, `Warning()`, `Error()`, `Complete()`
- Enables: Machine-parseable streaming output for automation

**PartitionScheme (`pkg/partition.go`):**
- Purpose: Represents A/B partition layout
- Fields: BootPartition, Root1Partition, Root2Partition, VarPartition
- Encryption: Tracks LUKS devices with UUIDs and mapper paths
- Layout: ESP (FAT32) + Root1 + Root2 + Var (shared)

## Entry Points

**main.go:**
- Location: `main.go`
- Triggers: CLI invocation
- Responsibilities: Set version info, call `cmd.Execute()`

**cmd.Execute():**
- Location: `cmd/root.go`
- Triggers: All CLI commands
- Responsibilities: Parse commands, initialize Cobra, handle signals

**Install Command:**
- Location: `cmd/install.go`
- Triggers: `nbc install <image> <device>`
- Responsibilities: Create `Installer`, configure callbacks, run installation

**Update Command:**
- Location: `cmd/update.go`
- Triggers: `nbc update [--image <ref>]`
- Responsibilities: Create `SystemUpdater`, check for updates, perform update

**Status Command:**
- Location: `cmd/status.go`
- Triggers: `nbc status`
- Responsibilities: Read system config, check for available updates, report state

## Error Handling

**Strategy:** Wrapped errors with context using `fmt.Errorf("context: %w", err)`

**Patterns:**
- Functions return `error` as last return value
- Callbacks (`OnError`) notify before returning
- Critical operations have defer-based cleanup (unmount, close LUKS)
- Lock acquisition prevents concurrent operations (`AcquireSystemLock`, `AcquireCacheLock`)

**Recovery:**
- Failed installations leave disk in wiped state (safe to retry)
- Failed updates leave inactive partition in unknown state (active still bootable)
- "Do NOT reboot" warnings on critical failures

## Cross-Cutting Concerns

**Logging:** 
- Verbose mode controlled by `--verbose` flag
- Progress callbacks for structured output
- ProgressReporter for JSON Lines streaming

**Validation:**
- `InstallConfig.Validate()` checks required fields, mutual exclusivity
- `CheckRequiredTools()` verifies system tool availability
- `ValidateDisk()` checks device exists and has minimum size
- `VerifyExtraction()` validates extracted filesystem integrity

**Authentication:**
- Container registry auth via `authn.DefaultKeychain` (respects Docker/Podman credentials)
- LUKS passphrase from user input or TPM2 auto-unlock

**Concurrency:**
- File-based locking: `/var/lock/nbc.lock` (system), `/var/lock/nbc-cache.lock` (cache)
- Context cancellation support via `context.Context` passed through operations
- Cancellation checks between major steps

---

*Architecture analysis: 2026-01-26*
