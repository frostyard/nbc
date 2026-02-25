# Refactoring Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the nbc codebase for maintainability, consistency, and modern Go standards — eliminating code duplication between install/update flows, introducing a Reporter interface, removing deprecated code, adding context propagation throughout, and encapsulating CLI flags.

**Architecture:** The refactoring proceeds in 7 dependency-ordered phases. Phase 1 (Reporter interface) is foundational — all subsequent phases depend on it. Phase 4 (workflow pattern) is the largest and highest-risk phase. Each phase produces independently compilable, testable commits.

**Tech Stack:** Go 1.26, Cobra CLI, go-containerregistry. Tests via `make test-unit` (no root required). Build via `make build`. Lint via `make lint`.

**Design doc:** `docs/plans/2026-02-25-refactoring-design.md`

---

## Phase 1: Reporter Interface

**Goal:** Replace the concrete `*ProgressReporter` with a `Reporter` interface, enabling clean separation of text vs JSON output modes and eliminating the callback adapter layer.

### Task 1.1: Create Reporter interface and implementations

**Files:**
- Create: `pkg/reporter.go`
- Test: `pkg/reporter_test.go`

**Step 1: Write the failing tests**

```go
// pkg/reporter_test.go
package pkg

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/types"
)

func TestTextReporter_Step(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Step(1, 3, "Creating partitions")

	got := buf.String()
	want := "Step 1/3: Creating partitions...\n"
	if got != want {
		t.Errorf("Step output = %q, want %q", got, want)
	}
}

func TestTextReporter_StepAddsNewlineAfterFirst(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Step(1, 3, "First")
	r.Step(2, 3, "Second")

	got := buf.String()
	if !strings.Contains(got, "\nStep 2/3") {
		t.Errorf("Expected blank line before step 2, got %q", got)
	}
}

func TestTextReporter_Message(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Message("Installing %s", "grub")

	got := buf.String()
	want := "  Installing grub\n"
	if got != want {
		t.Errorf("Message output = %q, want %q", got, want)
	}
}

func TestTextReporter_Warning(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Warning("disk is slow")

	got := buf.String()
	want := "Warning: disk is slow\n"
	if got != want {
		t.Errorf("Warning output = %q, want %q", got, want)
	}
}

func TestJSONReporter_Step(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)
	r.Step(1, 3, "Creating partitions")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if event.Type != types.EventTypeStep {
		t.Errorf("Type = %q, want %q", event.Type, types.EventTypeStep)
	}
	if event.Step != 1 {
		t.Errorf("Step = %d, want 1", event.Step)
	}
	if event.StepName != "Creating partitions" {
		t.Errorf("StepName = %q, want %q", event.StepName, "Creating partitions")
	}
}

func TestNoopReporter(t *testing.T) {
	r := &NoopReporter{}
	// Should not panic
	r.Step(1, 1, "test")
	r.Message("msg")
	r.Warning("warn")
	r.Error(nil, "err")
	r.Progress(50, "half")
	r.Complete("done", nil)
	if r.IsJSON() {
		t.Error("NoopReporter.IsJSON() should return false")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/... -run "^Test(Text|JSON|Noop)Reporter" -count=1`
Expected: FAIL — types not defined

**Step 3: Write the Reporter interface and implementations**

```go
// pkg/reporter.go
package pkg

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/frostyard/nbc/pkg/types"
)

// Reporter is the interface for all progress/status output from pkg functions.
// All output from business logic flows through this interface — no direct
// fmt.Print calls outside of Reporter implementations.
type Reporter interface {
	// Step reports the start of a major step (e.g., "Creating partitions").
	Step(step, total int, name string)
	// Progress reports progress within a step (0-100 percent).
	Progress(percent int, message string)
	// Message reports an indented informational message.
	Message(format string, args ...any)
	// MessagePlain reports a non-indented informational message.
	MessagePlain(format string, args ...any)
	// Warning reports a warning.
	Warning(format string, args ...any)
	// Error reports an error with context.
	Error(err error, message string)
	// Complete reports successful completion with optional structured details.
	Complete(message string, details any)
	// IsJSON returns true if output is machine-readable JSON.
	IsJSON() bool
}

// TextReporter writes human-readable progress output.
type TextReporter struct {
	w         io.Writer
	stepCount int // tracks steps for blank-line logic
}

// NewTextReporter creates a Reporter that writes human-readable text to w.
func NewTextReporter(w io.Writer) *TextReporter {
	return &TextReporter{w: w}
}

func (r *TextReporter) Step(step, total int, name string) {
	if r.stepCount > 0 {
		fmt.Fprintln(r.w)
	}
	r.stepCount++
	fmt.Fprintf(r.w, "Step %d/%d: %s...\n", step, total, name)
}

func (r *TextReporter) Progress(percent int, message string) {
	if message != "" {
		fmt.Fprintf(r.w, "  %s\n", message)
	}
}

func (r *TextReporter) Message(format string, args ...any) {
	fmt.Fprintf(r.w, "  %s\n", fmt.Sprintf(format, args...))
}

func (r *TextReporter) MessagePlain(format string, args ...any) {
	fmt.Fprintln(r.w, fmt.Sprintf(format, args...))
}

func (r *TextReporter) Warning(format string, args ...any) {
	fmt.Fprintf(r.w, "Warning: %s\n", fmt.Sprintf(format, args...))
}

func (r *TextReporter) Error(err error, message string) {
	fmt.Fprintf(r.w, "Error: %s: %v\n", message, err)
}

func (r *TextReporter) Complete(message string, _ any) {
	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, "=================================================================")
	fmt.Fprintln(r.w, message)
	fmt.Fprintln(r.w, "=================================================================")
}

func (r *TextReporter) IsJSON() bool { return false }

// JSONReporter writes JSON Lines progress events.
type JSONReporter struct {
	w       io.Writer
	mu      sync.Mutex
	encoder *json.Encoder
}

// NewJSONReporter creates a Reporter that writes JSON Lines to w.
func NewJSONReporter(w io.Writer) *JSONReporter {
	return &JSONReporter{
		w:       w,
		encoder: json.NewEncoder(w),
	}
}

func (r *JSONReporter) emit(event types.ProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	_ = r.encoder.Encode(event)
}

func (r *JSONReporter) Step(step, total int, name string) {
	r.emit(types.ProgressEvent{
		Type:       types.EventTypeStep,
		Step:       step,
		TotalSteps: total,
		StepName:   name,
	})
}

func (r *JSONReporter) Progress(percent int, message string) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeProgress,
		Percent: percent,
		Message: message,
	})
}

func (r *JSONReporter) Message(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeMessage,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) MessagePlain(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeMessage,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) Warning(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeWarning,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) Error(err error, message string) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeError,
		Message: message,
		Details: map[string]string{"error": err.Error()},
	})
}

func (r *JSONReporter) Complete(message string, details any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeComplete,
		Message: message,
		Details: details,
	})
}

func (r *JSONReporter) IsJSON() bool { return true }

// NoopReporter discards all output. Useful for tests.
type NoopReporter struct{}

func (r *NoopReporter) Step(int, int, string)              {}
func (r *NoopReporter) Progress(int, string)               {}
func (r *NoopReporter) Message(string, ...any)             {}
func (r *NoopReporter) MessagePlain(string, ...any)        {}
func (r *NoopReporter) Warning(string, ...any)             {}
func (r *NoopReporter) Error(error, string)                {}
func (r *NoopReporter) Complete(string, any)               {}
func (r *NoopReporter) IsJSON() bool                       { return false }
```

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/... -run "^Test(Text|JSON|Noop)Reporter" -count=1`
Expected: PASS

**Step 5: Commit**

```
feat: add Reporter interface with Text, JSON, and Noop implementations
```

---

### Task 1.2: Migrate all `*ProgressReporter` parameters to `Reporter` interface

**Files:**
- Modify: `pkg/partition.go` — all functions taking `*ProgressReporter`
- Modify: `pkg/disk.go` — `WipeDisk`
- Modify: `pkg/container.go` — `SetProgress`, `CreateFstab`, `SetupSystemDirectories`, `PrepareMachineID`
- Modify: `pkg/config.go` — `InstallTmpfilesConfig`, `WriteSystemConfig`, `WriteSystemConfigToVar`, `UpdateSystemConfigImageRef`
- Modify: `pkg/dracut.go` — all functions
- Modify: `pkg/etc_persistence.go` — all functions
- Modify: `pkg/luks.go` — all functions
- Modify: `pkg/loopback.go` — `SetupLoopbackInstall`, `Cleanup`
- Modify: `pkg/bootc.go` — `SetRootPasswordInTarget`, `PullImage`
- Modify: `pkg/bootloader.go` — `SetProgress` and internal usage
- Modify: `pkg/cache.go` — `Download`, `Remove`, `Clear`
- Modify: `pkg/device_detect.go` — `GetCurrentBootDeviceInfo`

**Step 1: Mechanical replacement**

In every file listed above, change `progress *ProgressReporter` to `progress Reporter` and `p *ProgressReporter` to `p Reporter` in function signatures. For struct fields like `Progress *ProgressReporter`, change to `Progress Reporter`.

For `SetProgress(p *ProgressReporter)` methods (on `BootloaderInstaller` and `ContainerExtractor`), change the parameter type to `Reporter`.

The implementations already call the same method names (`Message`, `Warning`, `Step`, etc.) — the interface matches the concrete type's method set, so no call sites inside these functions need to change.

**Step 2: Update Installer struct**

In `pkg/install.go`, change the `progress` field from `*ProgressReporter` to `Reporter`:

```go
// Line 189: change from
progress  *ProgressReporter
// to
progress  Reporter
```

Update `NewInstaller` (line 270) to create the appropriate reporter:

```go
// Replace: progress: NewProgressReporter(cfg.JSONOutput, 6),
var reporter Reporter
if cfg.JSONOutput {
    reporter = NewJSONReporter(os.Stdout)
} else {
    reporter = NewTextReporter(os.Stdout)
}
return &Installer{
    config:   cfg,
    progress: reporter,
}, nil
```

**Step 3: Update SystemUpdater similarly**

Find where `SystemUpdater` stores its reporter and update to use the interface.

**Step 4: Run full test suite**

Run: `make test-unit`
Expected: PASS (all unit tests)

Run: `make build`
Expected: Compiles cleanly

**Step 5: Commit**

```
refactor: migrate all ProgressReporter params to Reporter interface
```

---

### Task 1.3: Delete callbacks, adapter, and backward-compat layer

**Files:**
- Modify: `pkg/install.go` — delete lines 131-150 (InstallCallbacks), 274-278 (SetCallbacks), 783-865 (callback helpers + adapter), 867-919 (CreateCLICallbacks), 777-781 (asLegacyProgress)
- Modify: `pkg/progress.go` — delete lines 1-157 (entire file becomes unnecessary; the old ProgressReporter, type aliases, and const re-exports)
- Modify: `cmd/install.go` — replace `CreateCLICallbacks` + `SetCallbacks` with direct Reporter creation
- Modify: `cmd/interactive_install.go` — same replacement
- Modify: `pkg/install_test.go` — update tests that use `SetCallbacks`, `InstallCallbacks`, `callbackProgressAdapter`

**Step 1: Update cmd/install.go**

Replace the callback pattern (around lines 111-114):

```go
// Before:
callbacks := pkg.CreateCLICallbacks(jsonOutput)
installer.SetCallbacks(callbacks)

// After: (reporter is now set by NewInstaller via JSONOutput config field)
// No callback setup needed — delete these lines
```

**Step 2: Update cmd/interactive_install.go**

Replace callback usage (around lines 495-496):

```go
// Before:
callbacks := pkg.CreateCLICallbacks(false)
installer.SetCallbacks(callbacks)

// After:
// No callback setup needed — delete these lines
```

**Step 3: Refactor Installer.Install() to use reporter directly**

Replace all `i.callOnStep(...)` calls with `i.progress.Step(...)`, all `i.callOnMessage(...)` with `i.progress.Message(...)`, all `i.callOnWarning(...)` with `i.progress.Warning(...)`, all `i.callOnError(...)` with `i.progress.Error(...)`. Replace `i.asLegacyProgress()` with `i.progress`.

**Step 4: Delete the callback infrastructure**

- Delete `InstallCallbacks` struct (lines 131-150)
- Delete `SetCallbacks` method (lines 274-278)
- Delete `callOnStep`, `callOnProgress`, `callOnMessage`, `callOnWarning`, `callOnError` (lines 783-814)
- Delete `callbackProgressAdapter` struct and all its methods (lines 816-865)
- Delete `CreateCLICallbacks` function (lines 867-919)
- Delete `asLegacyProgress` method (lines 777-781)
- Remove `callbacks` and `progressAdapter` fields from `Installer` struct (lines 184, 192)

**Step 5: Delete old progress.go**

Delete the entire `pkg/progress.go` file. The `Reporter` interface and implementations in `pkg/reporter.go` replace it entirely.

**Step 6: Update tests**

In `pkg/install_test.go`, update tests that reference `InstallCallbacks`, `SetCallbacks`, or `callbackProgressAdapter`. Replace with tests that verify Reporter output.

**Step 7: Run tests and build**

Run: `make test-unit && make build && make lint`
Expected: All pass

**Step 8: Commit**

```
refactor: remove InstallCallbacks, adapter layer, and old ProgressReporter

The Reporter interface replaces all callback-based progress reporting.
Installer now uses Reporter directly — no more indirection layers.
```

---

## Phase 2: Delete BootcInstaller

### Task 2.1: Relocate standalone functions from bootc.go

**Files:**
- Create: `pkg/tools.go` (for `CheckRequiredTools`)
- Modify: `pkg/container.go` (move `PullImage` here)
- Create: `pkg/system.go` (for `SetRootPasswordInTarget`)
- Modify: `pkg/bootc.go` — remove the relocated functions

**Step 1: Move `CheckRequiredTools` to `pkg/tools.go`**

Copy `CheckRequiredTools()` (bootc.go lines 135-155) to a new `pkg/tools.go`. No signature changes needed.

**Step 2: Move `PullImage` to `pkg/container.go`**

Copy the standalone `PullImage()` function (bootc.go lines 157-200) to `pkg/container.go`. Update its signature to use `Reporter` instead of `*ProgressReporter` (should already be done from Phase 1).

**Step 3: Move `SetRootPasswordInTarget` to `pkg/system.go`**

Copy `SetRootPasswordInTarget()` (bootc.go lines 107-133) to a new `pkg/system.go`.

**Step 4: Run tests**

Run: `make test-unit && make build`
Expected: PASS

**Step 5: Commit**

```
refactor: relocate standalone functions from bootc.go

CheckRequiredTools → pkg/tools.go
PullImage → pkg/container.go
SetRootPasswordInTarget → pkg/system.go
```

---

### Task 2.2: Delete BootcInstaller and its tests

**Files:**
- Delete: `pkg/bootc.go`
- Modify: `pkg/bootc_test.go` — keep only tests for the relocated functions, move them to corresponding test files
- Modify: `Makefile` — update `_test-install` target (line 54) which runs `TestBootcInstaller`

**Step 1: Move retained tests**

Move `TestSetRootPasswordInTarget_*` tests from `pkg/bootc_test.go` to `pkg/system_test.go`.
Move `TestCheckRequiredTools` (if it exists) to `pkg/tools_test.go`.

**Step 2: Delete pkg/bootc.go and pkg/bootc_test.go**

**Step 3: Update Makefile**

Line 54 references `TestBootcInstaller` — remove or update this target:

```makefile
# Before:
_test-install: ## Internal target for install tests
	@go test -v ./pkg/... -run "^(TestBootcInstaller)" -timeout 20m

# After:
_test-install: ## Internal target for install tests
	@go test -v ./pkg/... -run "^(TestInstaller)" -timeout 20m
```

**Step 4: Run tests and build**

Run: `make test-unit && make build && make lint`
Expected: PASS

**Step 5: Commit**

```
refactor: delete deprecated BootcInstaller (645 LOC)

The newer Installer API (NewInstaller) fully replaces it.
Standalone functions preserved in tools.go, container.go, system.go.
```

---

## Phase 3: Consolidate Duplicate Types

### Task 3.1: Remove duplicate RebootPendingInfo

**Files:**
- Modify: `pkg/config.go` — delete `RebootPendingInfo` struct (lines 255-261), update `WriteRebootRequiredMarker` and `ReadRebootRequiredMarker` to use `types.RebootPendingInfo`
- Modify: `pkg/update.go` — update usage at line 854 to use `types.RebootPendingInfo`

**Step 1: Update config.go**

Add `"github.com/frostyard/nbc/pkg/types"` import if not present.

Delete lines 255-261 (the `RebootPendingInfo` struct definition).

Update function signatures:

```go
// Line 264: change from
func WriteRebootRequiredMarker(info *RebootPendingInfo) error {
// to
func WriteRebootRequiredMarker(info *types.RebootPendingInfo) error {

// Line 278: change from
func ReadRebootRequiredMarker() (*RebootPendingInfo, error) {
// to
func ReadRebootRequiredMarker() (*types.RebootPendingInfo, error) {

// Line 287: change from
var info RebootPendingInfo
// to
var info types.RebootPendingInfo
```

**Step 2: Update update.go line 854**

```go
// Change from:
rebootInfo := &RebootPendingInfo{
// to:
rebootInfo := &types.RebootPendingInfo{
```

**Step 3: Run tests and build**

Run: `make test-unit && make build`
Expected: PASS

**Step 4: Commit**

```
refactor: consolidate RebootPendingInfo to single definition in pkg/types
```

---

## Phase 4: Workflow Pattern + Context Propagation

This is the largest phase. Split into sub-tasks.

### Task 4.1: Add context.Context to functions missing it

**Files:**
- Modify: `pkg/cache.go` — `Download`, `Remove`, `Clear`
- Modify: `pkg/config.go` — `InstallTmpfilesConfig`, `WriteSystemConfig`, `WriteSystemConfigToVar`, `UpdateSystemConfigImageRef`
- Modify: `pkg/device_detect.go` — `GetCurrentBootDeviceInfo`
- Modify: `pkg/dracut.go` — `InstallDracutEtcOverlay`, `VerifyDracutEtcOverlay`
- Modify: `pkg/etc_persistence.go` — all 7 exported functions
- Modify: `pkg/container.go` — `SetupSystemDirectories`, `PrepareMachineID`
- Modify: `pkg/loopback.go` — `SetupLoopbackInstall`, `LoopbackDevice.Cleanup`
- Modify: `pkg/system.go` — `SetRootPasswordInTarget`
- Modify: All callers of the above functions

**Step 1: Mechanical change**

For each function, add `ctx context.Context` as the first parameter. Convert any `exec.Command(...)` inside to `exec.CommandContext(ctx, ...)`.

Example for `SetRootPasswordInTarget`:

```go
// Before:
func SetRootPasswordInTarget(targetDir, password string, dryRun bool, progress Reporter) error {

// After:
func SetRootPasswordInTarget(ctx context.Context, targetDir, password string, dryRun bool, progress Reporter) error {
```

**Step 2: Update all call sites**

Search for every call to these 23 functions and add `ctx` as the first argument. Most callers already have `ctx` available — the Installer.Install() and SystemUpdater.Update() methods receive it.

**Step 3: Run tests and build**

Run: `make test-unit && make build`
Expected: PASS

**Step 4: Commit**

```
refactor: add context.Context to all exported I/O functions

23 exported functions in pkg/ now accept context as first parameter,
enabling cancellation propagation throughout the entire call chain.
```

---

### Task 4.2: Create Workflow type

**Files:**
- Create: `pkg/workflow.go`
- Test: `pkg/workflow_test.go`

**Step 1: Write the failing tests**

```go
// pkg/workflow_test.go
package pkg

import (
	"context"
	"errors"
	"testing"
)

func TestWorkflow_RunsAllSteps(t *testing.T) {
	var ran []string
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("step1", func(ctx context.Context, s *WorkflowState) error {
		ran = append(ran, "step1")
		return nil
	})
	w.AddStep("step2", func(ctx context.Context, s *WorkflowState) error {
		ran = append(ran, "step2")
		return nil
	})

	if err := w.Run(context.Background(), &WorkflowState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ran) != 2 || ran[0] != "step1" || ran[1] != "step2" {
		t.Errorf("ran = %v, want [step1 step2]", ran)
	}
}

func TestWorkflow_StopsOnError(t *testing.T) {
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("good", func(ctx context.Context, s *WorkflowState) error {
		return nil
	})
	w.AddStep("bad", func(ctx context.Context, s *WorkflowState) error {
		return errors.New("boom")
	})
	w.AddStep("never", func(ctx context.Context, s *WorkflowState) error {
		t.Error("should not reach this step")
		return nil
	})

	err := w.Run(context.Background(), &WorkflowState{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) && !containsString(err.Error(), "boom") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWorkflow_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	w := NewWorkflow(&NoopReporter{})
	w.AddStep("unreachable", func(ctx context.Context, s *WorkflowState) error {
		t.Error("step should not run when context is cancelled")
		return nil
	})

	err := w.Run(ctx, &WorkflowState{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestWorkflow_ReportsSteps(t *testing.T) {
	var steps []string
	reporter := &stepTrackingReporter{steps: &steps}
	w := NewWorkflow(reporter)
	w.AddStep("First step", func(ctx context.Context, s *WorkflowState) error { return nil })
	w.AddStep("Second step", func(ctx context.Context, s *WorkflowState) error { return nil })

	if err := w.Run(context.Background(), &WorkflowState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 2 {
		t.Errorf("expected 2 step reports, got %d", len(steps))
	}
}

// helper
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && strings.Contains(s, substr))
}

type stepTrackingReporter struct {
	NoopReporter
	steps *[]string
}

func (r *stepTrackingReporter) Step(step, total int, name string) {
	*r.steps = append(*r.steps, name)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/... -run "^TestWorkflow" -count=1`
Expected: FAIL

**Step 3: Implement Workflow**

```go
// pkg/workflow.go
package pkg

import (
	"context"
	"fmt"
)

// StepFunc is a single step in a workflow.
type StepFunc func(ctx context.Context, state *WorkflowState) error

type namedStep struct {
	name string
	fn   StepFunc
}

// Workflow orchestrates a sequence of named steps with progress reporting
// and context cancellation.
type Workflow struct {
	steps    []namedStep
	reporter Reporter
}

// NewWorkflow creates a Workflow that reports progress via the given Reporter.
func NewWorkflow(reporter Reporter) *Workflow {
	return &Workflow{reporter: reporter}
}

// AddStep appends a named step to the workflow.
func (w *Workflow) AddStep(name string, fn StepFunc) {
	w.steps = append(w.steps, namedStep{name: name, fn: fn})
}

// Run executes all steps in order. It checks context before each step
// and reports step progress through the Reporter.
func (w *Workflow) Run(ctx context.Context, state *WorkflowState) error {
	total := len(w.steps)
	for i, step := range w.steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.reporter.Step(i+1, total, step.name)
		if err := step.fn(ctx, state); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

// WorkflowState holds shared mutable state passed between workflow steps.
type WorkflowState struct {
	// Device is the target block device path.
	Device string
	// MountPoint is the temporary mount point for the target filesystem.
	MountPoint string
	// Scheme holds partition layout information.
	Scheme *PartitionScheme
	// ImageRef is the container image reference.
	ImageRef string
	// ImageDigest is the digest of the image being installed/updated.
	ImageDigest string
	// FilesystemType is "ext4" or "btrfs".
	FilesystemType string
	// KernelArgs holds additional kernel command line arguments.
	KernelArgs []string
	// DryRun indicates whether to simulate operations.
	DryRun bool
	// Verbose enables additional output.
	Verbose bool
	// Reporter is the output reporter for the workflow.
	Reporter Reporter
	// Encrypted indicates whether LUKS encryption is enabled.
	Encrypted bool
	// Passphrase is the LUKS passphrase (if encrypted).
	Passphrase string
	// TPM2 indicates whether TPM2 auto-unlock is enabled.
	TPM2 bool
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/... -run "^TestWorkflow" -count=1`
Expected: PASS

**Step 5: Commit**

```
feat: add Workflow type for composable step-based orchestration
```

---

### Task 4.3: Extract shared steps from install and update

**Files:**
- Create: `pkg/steps.go` — shared step implementations
- Test: `pkg/steps_test.go` — unit tests for shared steps
- Modify: `pkg/install.go` — refactor `Install()` to use workflow
- Modify: `pkg/update.go` — refactor `Update()` / `PerformUpdate()` to use workflow

**Step 1: Identify shared logic**

Read both `Install()` and the update flow carefully to identify the exact shared sequences. The shared steps are:

1. `stepCreatePartitions` — calls `CreatePartitions`
2. `stepFormatPartitions` — calls `FormatPartitions`
3. `stepMountPartitions` — calls `MountPartitions`
4. `stepExtractContainer` — creates `ContainerExtractor` and calls `Extract`
5. `stepSetupSystemFiles` — calls `CreateFstab`, `SetupSystemDirectories`, `PrepareMachineID`, `PopulateEtcLower`, `InstallTmpfilesConfig`, `InstallEtcMountUnit`, `SavePristineEtc`
6. `stepInstallBootloader` — creates `BootloaderInstaller` and calls `Install`

**Step 2: Implement shared steps in `pkg/steps.go`**

Each step function takes `(ctx context.Context, state *WorkflowState) error` and delegates to the existing lower-level functions, passing `state.Reporter`, `state.DryRun`, etc. from the workflow state.

**Step 3: Refactor Installer.Install()**

Replace the inline step logic with workflow composition:

```go
func (i *Installer) Install(ctx context.Context) (*InstallResult, error) {
    state := &WorkflowState{
        MountPoint:     i.config.MountPoint,
        FilesystemType: i.config.FilesystemType,
        KernelArgs:     i.config.KernelArgs,
        DryRun:         i.config.DryRun,
        Verbose:        i.config.Verbose,
        Reporter:       i.progress,
        // ... populate encryption fields if applicable
    }

    // Pre-workflow setup (device, lock, prerequisites)
    device, err := i.setupDevice(ctx)
    if err != nil {
        return result, err
    }
    state.Device = device

    w := NewWorkflow(i.progress)
    w.AddStep("Creating partitions", stepCreatePartitions)
    w.AddStep("Formatting partitions", stepFormatPartitions)
    w.AddStep("Mounting partitions", stepMountPartitions)
    w.AddStep("Extracting container", i.stepExtractContainer)
    w.AddStep("Configuring system", stepSetupSystemFiles)
    w.AddStep("Installing bootloader", stepInstallBootloader)

    if err := w.Run(ctx, state); err != nil {
        return result, err
    }
    // ...
}
```

**Step 4: Refactor SystemUpdater.Update() similarly**

The update flow reuses the shared steps but adds its own steps for detecting the active partition, pulling the image, and marking reboot pending.

**Step 5: Run tests**

Run: `make test-unit && make build && make lint`
Expected: PASS

**Step 6: Commit**

```
refactor: extract shared install/update steps into workflow

Shared logic (partitioning, formatting, mounting, system config,
bootloader) is now written once and composed by both flows.
Install/update parity enforced by construction.
```

---

## Phase 5: Eliminate Raw Output

### Task 5.1: Replace all fmt.Print calls in pkg/ with Reporter

**Files:**
- Modify: `pkg/update.go` — 8 fmt.Print + 4 stderr warnings
- Modify: `pkg/cache.go` — 7 fmt.Print calls
- Modify: `pkg/partition.go` — 3 stderr warnings
- Modify: `pkg/device_detect.go` — 7 stderr warnings
- Modify: `pkg/dracut.go` — 3 fmt.Print calls
- Modify: `pkg/luks.go` — 1 fmt.Print call
- Modify: `pkg/lint.go` — 2 fmt.Print calls
- Modify: `pkg/bootloader.go` — 2 fmt.Print calls

**Step 1: Audit and replace**

For each file, find every `fmt.Print*` or `fmt.Fprint*(os.Stderr, ...)` call. Replace with the appropriate Reporter method:

- `fmt.Printf("  %s\n", msg)` → `reporter.Message("%s", msg)`
- `fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)` → `reporter.Warning("%s", msg)`
- `fmt.Println(msg)` → `reporter.MessagePlain("%s", msg)`

Functions that don't currently receive a Reporter parameter need it added. Most were already updated in Phases 1 and 4.

**Step 2: Verify no raw output remains**

Run: `grep -rn 'fmt\.Print\|fmt\.Fprint' pkg/ --include='*.go' | grep -v _test.go | grep -v reporter.go`
Expected: No output (zero matches outside reporter.go and test files)

**Step 3: Run tests and build**

Run: `make test-unit && make build && make lint`
Expected: PASS

**Step 4: Commit**

```
refactor: eliminate all raw fmt.Print output from pkg/

All output now flows through the Reporter interface.
The only fmt.Print calls in pkg/ are inside TextReporter.
```

---

## Phase 6: Flag Structs in cmd/

### Task 6.1: Encapsulate install flags

**Files:**
- Modify: `cmd/install.go`

**Step 1: Create flags struct**

Replace the var block (lines 14-29) with:

```go
type installFlags struct {
	image            string
	device           string
	skipPull         bool
	kernelArgs       []string
	filesystem       string
	encrypt          bool
	passphrase       string
	keyfile          string
	tpm2             bool
	localImage       string
	rootPasswordFile string
	viaLoopback      string
	imageSize        int
	force            bool
}
```

**Step 2: Create parseInstallFlags helper**

```go
func parseInstallFlags(cmd *cobra.Command) installFlags {
	f := installFlags{}
	f.image, _ = cmd.Flags().GetString("image")
	f.device, _ = cmd.Flags().GetString("device")
	// ... etc for each flag
	return f
}
```

**Step 3: Update runInstall to accept the struct**

```go
func runInstall(cmd *cobra.Command, args []string) error {
	f := parseInstallFlags(cmd)
	return doInstall(cmd.Context(), f, viper.GetBool("verbose"), viper.GetBool("dry-run"), viper.GetBool("json"))
}
```

**Step 4: Update init() to use local flags (no var binding)**

Keep using `cmd.Flags().StringVarP(...)` but bind to local vars in `init()` — OR switch to `cmd.Flags().StringP(...)` without binding and use `GetString` in the parse function. The latter is cleaner.

**Step 5: Run tests and build**

Run: `make build && make lint`
Expected: PASS

**Step 6: Commit**

```
refactor: encapsulate install command flags into struct
```

---

### Task 6.2: Encapsulate update flags

Same pattern as 6.1 for `cmd/update.go`.

**Commit:** `refactor: encapsulate update command flags into struct`

### Task 6.3: Encapsulate remaining command flags

Apply the same pattern to: `cmd/cache.go`, `cmd/download.go`, `cmd/lint.go`, `cmd/validate.go`.

`cmd/status.go` and `cmd/list.go` have no custom flags — skip them.

**Commit:** `refactor: encapsulate remaining CLI command flags into structs`

---

## Phase 7: Cleanup Pass

### Task 7.1: Centralize constants and defaults

**Files:**
- Create: `pkg/defaults.go`

**Step 1: Collect scattered constants**

Search for magic numbers and default values across `pkg/`. Move them to `pkg/defaults.go`:

```go
// pkg/defaults.go
package pkg

const (
	DefaultFilesystemType = "btrfs"
	DefaultMountPoint     = "/tmp/nbc-install"
	DefaultLoopbackSizeGB = 50
	MinDiskSizeBytes      = 10 * 1024 * 1024 * 1024 // 10 GB
)
```

Update references throughout `pkg/` to use these constants.

**Step 2: Commit**

```
refactor: centralize default constants in pkg/defaults.go
```

---

### Task 7.2: Fix viper error handling

**Files:**
- Modify: `cmd/update.go` — line 79
- Modify: any other cmd/ files with `_ = viper.BindPFlag(...)`

**Step 1: Replace ignored errors**

```go
// Before:
_ = viper.BindPFlag("force", updateCmd.Flags().Lookup("force"))

// After:
if err := viper.BindPFlag("force", updateCmd.Flags().Lookup("force")); err != nil {
    panic(fmt.Sprintf("failed to bind flag: %v", err))
}
```

Use `panic` since this is init-time setup that should never fail.

**Step 2: Commit**

```
fix: handle viper.BindPFlag errors instead of ignoring them
```

---

### Task 7.3: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update documentation**

Update the Key Components section to reflect:
- `Reporter` interface (replaces `ProgressReporter`)
- `Workflow` type for step composition
- Deleted `BootcInstaller`
- Consolidated types

Update the Console Output section to reference `Reporter` interface instead of `ProgressReporter`.

Remove the note about Install/Update parity being manual — it's now enforced by shared steps.

**Step 2: Commit**

```
docs: update CLAUDE.md to reflect refactoring changes
```

---

## Verification Checklist

After all phases are complete:

1. `make build` — compiles cleanly
2. `make test-unit` — all unit tests pass
3. `make lint` — no lint issues
4. `make fmt` — no formatting changes
5. `grep -rn 'fmt\.Print\|fmt\.Fprint' pkg/ --include='*.go' | grep -v _test.go | grep -v reporter.go` — zero matches
6. `grep -rn 'BootcInstaller' pkg/` — zero matches
7. `grep -rn 'InstallCallbacks\|callbackProgressAdapter\|CreateCLICallbacks' pkg/` — zero matches
8. `grep -rn 'ProgressReporter' pkg/ --include='*.go' | grep -v _test.go` — zero matches (only Reporter interface)
