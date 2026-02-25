# Refactoring Design: Maintainability, Consistency & Modern Go

**Date:** 2026-02-25
**Branch:** emdash/refactoring-things-5u4
**Scope:** Comprehensive refactoring of `pkg/` and `cmd/` packages

## Goals

1. Reduce code duplication between install and update flows
2. Improve testability by decoupling business logic from system calls and global state
3. Remove deprecated code and consolidate duplicate types
4. Ensure consistent context propagation and output through a single Reporter interface
5. Eliminate all raw `fmt.Print*` output from business logic

## Current State Summary

- ~20,295 LOC across `cmd/` (2,496) and `pkg/` (16,409)
- 23 exported functions missing `context.Context`
- 35+ functions accepting concrete `*ProgressReporter` instead of an interface
- 48 raw `fmt.Print*` calls and 15 `fmt.Fprintf(os.Stderr, ...)` calls in `pkg/`
- 645 LOC deprecated `BootcInstaller` still present
- `RebootPendingInfo` duplicated in `pkg/config.go` and `pkg/types/types.go`
- Module-level flag variables in `cmd/` (5-15 per command file)
- `InstallCallbacks` + `callbackProgressAdapter` bridge layer (67 LOC)
- Install and update flows duplicate partitioning, formatting, mounting, kernel cmdline logic

## Phase 1: Reporter Interface

**Goal:** Replace concrete `*ProgressReporter` with a `Reporter` interface. This is foundational — every subsequent phase depends on it.

### New Types

```go
// pkg/reporter.go
type Reporter interface {
    Step(step, total int, name string)
    Progress(percent int, message string)
    Message(format string, args ...any)
    MessagePlain(format string, args ...any)
    Warning(format string, args ...any)
    Error(err error, message string)
    Complete(message string, details any)
    IsJSON() bool
}

type TextReporter struct { w io.Writer }
type JSONReporter struct { w io.Writer; mu sync.Mutex; encoder *json.Encoder }
type NoopReporter struct{}
```

### Deletions

- `InstallCallbacks` struct and all callback methods (`callOnStep`, `callOnMessage`, etc.)
- `SetCallbacks()` and `CreateCLICallbacks()`
- `callbackProgressAdapter` type and `newCallbackProgressAdapter()`
- Backward-compat type aliases in `progress.go` (lines 13-25):
  ```go
  type EventType = types.EventType
  type ProgressEvent = types.ProgressEvent
  // ...re-exported constants...
  ```

### Signature Changes

All 35+ functions change from `progress *ProgressReporter` to `progress Reporter`:

- `cache.go`: Download, Remove, Clear
- `config.go`: InstallTmpfilesConfig, WriteSystemConfig, WriteSystemConfigToVar, UpdateSystemConfigImageRef
- `device_detect.go`: GetCurrentBootDeviceInfo
- `dracut.go`: InstallDracutEtcOverlay, VerifyDracutEtcOverlay, RegenerateInitramfs
- `etc_persistence.go`: SetupEtcOverlay, SetupEtcPersistence, InstallEtcMountUnit, PopulateEtcLower, SavePristineEtc, MergeEtcFromActive, EnsureCriticalFilesInOverlay
- `bootc.go`: SetRootPasswordInTarget, PullImage
- `loopback.go`: SetupLoopbackInstall, LoopbackDevice.Cleanup
- `container.go`: SetupSystemDirectories, PrepareMachineID, CreateFstab
- `disk.go`: WipeDisk
- `partition.go`: CreatePartitions, SetupLUKS, FormatPartitions, MountPartitions, UnmountPartitions
- `luks.go`: CreateLUKSContainer, OpenLUKS, CloseLUKS
- `bootloader.go`: SetProgress method

### Callers Updated

- `cmd/install.go`: Create `TextReporter` or `JSONReporter` based on `--json` flag, pass to `NewInstaller`
- `cmd/update.go`: Same pattern
- `cmd/interactive_install.go`: Replace `InstallCallbacks` usage with Reporter
- `cmd/cache.go`, `cmd/download.go`: Pass Reporter to cache operations
- `Installer` and `SystemUpdater` structs: Store `Reporter` instead of `*ProgressReporter`

## Phase 2: Delete BootcInstaller

**Goal:** Remove 645 LOC of deprecated code.

### Deletions

- `pkg/bootc.go`: Delete `BootcInstaller` struct, `NewBootcInstaller()`, and its `Install()` method
- `pkg/bootc_test.go`: Delete tests for `BootcInstaller` (keep tests for standalone functions)

### Relocations

Three standalone functions in `bootc.go` are still used and must move:

| Function | New Location | Rationale |
|----------|-------------|-----------|
| `CheckRequiredTools()` | `pkg/tools.go` (new) | System utility, not tied to any installer |
| `PullImage()` | `pkg/container.go` | Container image operation |
| `SetRootPasswordInTarget()` | `pkg/system.go` (new) | System configuration operation |

### Test Relocations

Tests for the relocated functions move to corresponding `*_test.go` files.

## Phase 3: Consolidate Duplicate Types

**Goal:** Single source of truth for all types.

### Changes

- Delete `RebootPendingInfo` from `pkg/config.go` (lines 255-261)
- Use `types.RebootPendingInfo` everywhere (already used in `cmd/status.go` and `pkg/types/types.go`)
- Update `pkg/update.go` line 854 to use `types.RebootPendingInfo`
- Update `WriteRebootRequiredMarker` and `ReadRebootRequiredMarker` signatures

### Audit

Check for any other type duplication between `pkg/` and `pkg/types/`. The `ProgressEvent` and `EventType` aliases in `progress.go` were already cleaned in Phase 1.

## Phase 4: Workflow Pattern + Context Propagation

**Goal:** Extract shared install/update logic into composable steps and add `context.Context` everywhere.

### 4a: Context Propagation

Add `ctx context.Context` as first parameter to all 23 exported functions currently missing it:

**cache.go:**
- `Download(ctx, imageRef, progress)` → enables cancellable downloads
- `Remove(ctx, digestOrPrefix, progress)` → enables cancellable cleanup
- `Clear(ctx, progress)` → enables cancellable cleanup

**config.go:**
- `InstallTmpfilesConfig(ctx, targetDir, dryRun, progress)`
- `WriteSystemConfig(ctx, config, dryRun, progress)`
- `WriteSystemConfigToVar(ctx, varMountPoint, config, dryRun, progress)`
- `UpdateSystemConfigImageRef(ctx, imageRef, imageDigest, dryRun, progress)`

**device_detect.go:**
- `GetCurrentBootDeviceInfo(ctx, verbose, progress)`

**dracut.go:**
- `InstallDracutEtcOverlay(ctx, targetDir, dryRun, progress)`
- `VerifyDracutEtcOverlay(ctx, targetDir, dryRun, progress)`

**etc_persistence.go:**
- All 7 functions gain `ctx` as first parameter

**container.go:**
- `SetupSystemDirectories(ctx, targetDir, progress)`
- `PrepareMachineID(ctx, targetDir, progress)`

**loopback.go:**
- `SetupLoopbackInstall(ctx, imagePath, sizeGB, force, progress)`
- `LoopbackDevice.Cleanup(ctx, progress)`

**bootc.go (relocated functions):**
- `SetRootPasswordInTarget(ctx, targetDir, password, dryRun, progress)`

All `exec.Command()` calls inside these functions become `exec.CommandContext(ctx, ...)`.

### 4b: Workflow Abstraction

```go
// pkg/workflow.go
type StepFunc func(ctx context.Context, state *WorkflowState) error

type Workflow struct {
    steps    []namedStep
    reporter Reporter
}

type namedStep struct {
    name string
    fn   StepFunc
}

func NewWorkflow(reporter Reporter) *Workflow {
    return &Workflow{reporter: reporter}
}

func (w *Workflow) AddStep(name string, fn StepFunc) {
    w.steps = append(w.steps, namedStep{name: name, fn: fn})
}

func (w *Workflow) Run(ctx context.Context, state *WorkflowState) error {
    for i, step := range w.steps {
        if err := ctx.Err(); err != nil {
            return err
        }
        w.reporter.Step(i+1, len(w.steps), step.name)
        if err := step.fn(ctx, state); err != nil {
            return fmt.Errorf("step %q failed: %w", step.name, err)
        }
    }
    return nil
}

// WorkflowState holds shared state across workflow steps
type WorkflowState struct {
    Device         string
    MountPoint     string
    Scheme         *PartitionScheme
    ImageRef       string
    ImageDigest    string
    FilesystemType string
    KernelArgs     []string
    DryRun         bool
    Encrypted      bool
    Passphrase     string
    Reporter       Reporter
    // Extensible for flow-specific state
}
```

### Shared Steps (extracted from both flows)

```go
// pkg/steps.go - shared step implementations
func stepCreatePartitions(ctx context.Context, state *WorkflowState) error { ... }
func stepFormatPartitions(ctx context.Context, state *WorkflowState) error { ... }
func stepMountPartitions(ctx context.Context, state *WorkflowState) error { ... }
func stepExtractContainer(ctx context.Context, state *WorkflowState) error { ... }
func stepSetupSystemFiles(ctx context.Context, state *WorkflowState) error { ... }
func stepInstallBootloader(ctx context.Context, state *WorkflowState) error { ... }
func stepBuildKernelCmdline(ctx context.Context, state *WorkflowState) error { ... }
```

### Installer and SystemUpdater Composition

```go
// pkg/install.go
func (i *Installer) Install(ctx context.Context) (*InstallResult, error) {
    w := NewWorkflow(i.reporter)
    w.AddStep("Setup device", i.stepSetupDevice)
    w.AddStep("Create partitions", stepCreatePartitions)
    w.AddStep("Format partitions", stepFormatPartitions)
    w.AddStep("Mount partitions", stepMountPartitions)
    w.AddStep("Extract container", i.stepExtractContainer)
    w.AddStep("Configure system", stepSetupSystemFiles)
    w.AddStep("Install bootloader", stepInstallBootloader)
    // ...
    return result, w.Run(ctx, state)
}

// pkg/update.go
func (u *SystemUpdater) Update(ctx context.Context) error {
    w := NewWorkflow(u.reporter)
    w.AddStep("Detect active partition", u.stepDetectActive)
    w.AddStep("Pull image", u.stepPullImage)
    w.AddStep("Mount target", u.stepMountTarget)
    w.AddStep("Extract container", stepExtractContainer) // shared!
    w.AddStep("Configure system", stepSetupSystemFiles)  // shared!
    w.AddStep("Install bootloader", stepInstallBootloader) // shared!
    w.AddStep("Mark reboot pending", u.stepMarkReboot)
    // ...
    return w.Run(ctx, state)
}
```

The key win: `stepExtractContainer`, `stepSetupSystemFiles`, `stepInstallBootloader`, and `stepBuildKernelCmdline` are written once, used by both flows. The parity requirement from CLAUDE.md is enforced by construction.

## Phase 5: Eliminate Raw Output

**Goal:** Zero `fmt.Print*` calls in `pkg/` outside of `TextReporter`.

### Changes

- Replace all 48 `fmt.Print*` calls in `pkg/` with `reporter.Message()` or `reporter.Warning()`
- Replace all 15 `fmt.Fprintf(os.Stderr, ...)` calls with `reporter.Warning()`
- Functions that currently output but don't accept a Reporter must gain one (identified in Phase 1)
- The `TextReporter` implementation is the only place in `pkg/` that calls `fmt.Fprint*`

### Specific Hotspots

- `pkg/update.go`: 8 fmt.Print calls, 4 stderr warnings
- `pkg/partition.go`: 3 stderr warnings (partx/partprobe/udevadm failures)
- `pkg/device_detect.go`: 7 stderr warnings
- `pkg/cache.go`: 7 fmt.Print calls
- `pkg/install.go`: 9 fmt.Print calls

## Phase 6: Flag Structs in cmd/

**Goal:** Eliminate module-level flag variables, improve testability.

### Pattern

Each command file gets a flags struct:

```go
type installFlags struct {
    image, device, filesystem string
    kernelArgs                []string
    encrypt, skipPull, force  bool
    dryRun, jsonOutput        bool
    loopback                  string
    loopbackSize              int
    rootPassword              string
    verbose                   bool
}
```

The `RunE` function reads from `cmd.Flags()` into the struct:

```go
var installCmd = &cobra.Command{
    Use:   "install",
    Short: "Install a bootc container image to a disk",
    RunE: func(cmd *cobra.Command, args []string) error {
        f := parseInstallFlags(cmd)
        return runInstall(cmd.Context(), f)
    },
}
```

### Files Affected

- `cmd/install.go` — ~15 flag vars
- `cmd/update.go` — ~15 flag vars
- `cmd/interactive_install.go` — ~10 flag vars
- `cmd/cache.go` — ~5 flag vars
- `cmd/download.go` — ~5 flag vars
- `cmd/status.go` — ~5 flag vars
- `cmd/lint.go` — ~5 flag vars
- `cmd/list.go` — ~3 flag vars
- `cmd/validate.go` — ~3 flag vars

## Phase 7: Cleanup Pass

**Goal:** Final consistency sweep.

### Constants Centralization

Create `pkg/defaults.go` for scattered magic numbers:
- Partition sizes, minimum disk sizes
- Default filesystem type, mount points
- Timeouts, retry counts

### Viper Error Handling

Replace `_ = viper.BindPFlag(...)` with proper error handling:
```go
if err := viper.BindPFlag("image", cmd.Flags().Lookup("image")); err != nil {
    return fmt.Errorf("failed to bind flag: %w", err)
}
```

### Function Naming Audit

Ensure consistent verb-noun naming across `pkg/`:
- `Get*` for retrieval
- `Create*` / `Setup*` for creation
- `Delete*` / `Remove*` for deletion
- `Check*` / `Validate*` for validation

## Ordering & Dependencies

```
Phase 1 (Reporter Interface)
    ↓
Phase 2 (Delete BootcInstaller)
    ↓
Phase 3 (Consolidate Types)
    ↓
Phase 4 (Workflow + Context)  ← biggest phase, depends on Reporter interface
    ↓
Phase 5 (Eliminate Raw Output) ← depends on Reporter being everywhere
    ↓
Phase 6 (Flag Structs)  ← independent of pkg/ changes, but cleaner after
    ↓
Phase 7 (Cleanup)  ← final sweep
```

## Risk Mitigation

- Each phase is independently testable and committable
- Phases 1-3 are low-risk mechanical changes
- Phase 4 is highest risk — should be reviewed carefully for install/update parity
- All existing tests must pass after each phase
- Integration and QEMU tests validate end-to-end behavior
