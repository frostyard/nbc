# Extract Reporter to External Package — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the local Reporter implementation with `github.com/frostyard/std/reporter`.

**Architecture:** Delete local reporter code, add external dependency, update all imports. The external package has an identical API surface so this is purely mechanical.

**Tech Stack:** Go modules, `github.com/frostyard/std/reporter`

---

### Task 1: Add dependency and delete local reporter

**Files:**
- Delete: `pkg/reporter.go`
- Delete: `pkg/reporter_test.go`
- Modify: `pkg/types/types.go` (remove lines 20-47)
- Modify: `go.mod`, `go.sum`

**Step 1: Add the external dependency**

Run: `go get github.com/frostyard/std@latest`

**Step 2: Delete local reporter files**

```bash
rm pkg/reporter.go pkg/reporter_test.go
```

**Step 3: Remove ProgressEvent and EventType from types.go**

Remove the "Progress Events" section (lines 20-47) from `pkg/types/types.go`:
- The `EventType` type and its 6 constants
- The `ProgressEvent` struct
- The section header comments

These now live in `github.com/frostyard/std/reporter` as `reporter.EventType` and `reporter.ProgressEvent`.

**Step 4: Verify the project does NOT compile yet**

Run: `go build ./...`
Expected: compilation errors — all the files referencing `Reporter` can't find it.

---

### Task 2: Update pkg/ files to use external reporter

Every file in `pkg/` that references `Reporter`, `TextReporter`, `JSONReporter`, `NoopReporter`, `NewTextReporter`, or `NewJSONReporter` needs:

1. A new import: `"github.com/frostyard/std/reporter"`
2. All bare `Reporter` references become `reporter.Reporter`
3. All bare `NoopReporter` references become `reporter.NoopReporter`
4. All `NewTextReporter(...)` become `reporter.NewTextReporter(...)`
5. All `NewJSONReporter(...)` become `reporter.NewJSONReporter(...)`

**Files to update (non-test):**
- `pkg/bootloader.go` — field `Progress Reporter` → `Progress reporter.Reporter`, function params, `SetProgress`
- `pkg/cache.go` — field `Progress Reporter` → `Progress reporter.Reporter`, function params
- `pkg/config.go` — function params (4 functions)
- `pkg/container.go` — field `Progress reporter.Reporter`, `SetProgress`, constructor default `NewTextReporter`, function params (4 functions)
- `pkg/device_detect.go` — function params (2 functions)
- `pkg/disk.go` — function params
- `pkg/dracut.go` — function params (3 functions)
- `pkg/etc_persistence.go` — function params (7 functions)
- `pkg/install.go` — field `progress reporter.Reporter`, constructor creates reporter
- `pkg/lint.go` — field `progress reporter.Reporter`
- `pkg/loopback.go` — function params (2 functions)
- `pkg/luks.go` — function params (5 functions)
- `pkg/partition.go` — function params (5 functions)
- `pkg/steps.go` — function params (2 functions)
- `pkg/system.go` — function params
- `pkg/update.go` — field `Progress reporter.Reporter`, function params
- `pkg/workflow.go` — field `reporter reporter.Reporter`, `Reporter reporter.Reporter` in WorkflowState, constructor param

**Step 1: Update all pkg/ non-test files**

For each file, add `"github.com/frostyard/std/reporter"` to imports and replace every bare `Reporter` with `reporter.Reporter`, `NoopReporter` with `reporter.NoopReporter`, `NewTextReporter` with `reporter.NewTextReporter`, `NewJSONReporter` with `reporter.NewJSONReporter`.

**Step 2: Update all pkg/ test files**

Test files that reference reporter types:
- `pkg/bootloader_test.go`
- `pkg/cache_test.go`
- `pkg/config_test.go`
- `pkg/container_test.go`
- `pkg/dracut_test.go`
- `pkg/etc_lower_test.go`
- `pkg/etc_persistence_test.go`
- `pkg/install_test.go`
- `pkg/integration_test.go`
- `pkg/partition_test.go`
- `pkg/system_test.go`
- `pkg/update_test.go`
- `pkg/workflow_test.go` — also update `stepTrackingReporter` to embed `reporter.NoopReporter`

Most test files just use `NoopReporter{}` → `reporter.NoopReporter{}`.

---

### Task 3: Update cmd/ files

**Files:**
- `cmd/install.go` — `pkg.Reporter` → `reporter.Reporter`, `pkg.NewTextReporter` → `reporter.NewTextReporter`, `pkg.NewJSONReporter` → `reporter.NewJSONReporter`
- `cmd/update.go` — same pattern, plus `pkg.NoopReporter{}` → `reporter.NoopReporter{}`
- `cmd/cache.go` — same pattern
- `cmd/download.go` — same pattern

For each file, add `"github.com/frostyard/std/reporter"` import and replace `pkg.Reporter` → `reporter.Reporter`, `pkg.NewTextReporter` → `reporter.NewTextReporter`, `pkg.NewJSONReporter` → `reporter.NewJSONReporter`, `pkg.NoopReporter` → `reporter.NoopReporter`.

---

### Task 4: Verify, format, lint, test

**Step 1: Build**

Run: `make build`
Expected: compiles successfully

**Step 2: Format and lint**

Run: `make fmt && make lint`
Expected: no issues

**Step 3: Run unit tests**

Run: `make test-unit`
Expected: all tests pass

**Step 4: Tidy modules**

Run: `go mod tidy`

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor: replace local Reporter with github.com/frostyard/std/reporter"
```
