---
phase: 01-testing-reliability
plan: 02
subsystem: testing
tags: [incus, goldie, vm-testing, golden-files, test-fixtures]

# Dependency graph
requires:
  - phase: 01-01
    provides: test infrastructure dependencies (incus/v6, goldie/v2)
provides:
  - IncusFixture for VM lifecycle management in tests
  - Golden file testing utilities with normalization
  - Test isolation via t.Cleanup and snapshots
affects: [01-03, 01-04, 01-05, 01-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "IncusFixture pattern: wrap Incus client with test-specific helpers"
    - "t.Cleanup registration before resource creation for reliable cleanup"
    - "NormalizeOutput pattern: deterministic golden file comparison"

key-files:
  created:
    - pkg/testutil/incus.go
    - pkg/testutil/golden.go
  modified: []

key-decisions:
  - "Cleanup is silent best-effort - errors ignored to not mask test failures"
  - "VM names include test name and PID for uniqueness"
  - "Normalize timestamps, UUIDs, loop devices, temp paths in golden files"
  - "Use 'degraded' as acceptable systemd state (some services may fail in VMs)"

patterns-established:
  - "IncusFixture: NewIncusFixture(t) → cleanup registered → CreateVM → operations"
  - "Golden files: go test -update ./... to regenerate"

# Metrics
duration: 2min
completed: 2026-01-26
---

# Phase 01 Plan 02: Test Fixtures Summary

**Incus VM fixture with cleanup registration and golden file helpers with timestamp/UUID normalization**

## Performance

- **Duration:** 2 min
- **Started:** 2026-01-26T21:09:41Z
- **Completed:** 2026-01-26T21:12:04Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- IncusFixture wrapping incus.InstanceServer with test-specific helpers
- Cleanup registered via t.Cleanup() before any resource creation
- CreateVM, WaitForReady, ExecCommand, PushFile, AttachDisk methods
- CreateSnapshot/RestoreSnapshot for test isolation
- Golden file helpers with deterministic normalization
- NormalizeOutput handles timestamps, UUIDs, loop devices, temp paths

## Task Commits

Each task was committed atomically:

1. **Task 1: Create Incus test fixture** - `325642b` (feat)
2. **Task 2: Create golden file helpers** - `73539b3` (feat)

## Files Created/Modified

- `pkg/testutil/incus.go` - Incus VM fixture (440 lines)
  - IncusFixture struct wrapping incus.InstanceServer
  - NewIncusFixture connects to local socket, skips if unavailable
  - Full VM lifecycle: CreateVM, WaitForReady, Cleanup
  - File and disk operations: PushFile, AttachDisk
  - Snapshot operations: CreateSnapshot, RestoreSnapshot
- `pkg/testutil/golden.go` - Golden file utilities (99 lines)
  - NewGolden creates configured goldie instance
  - NormalizeOutput for deterministic comparisons
  - AssertGolden convenience wrapper

## Decisions Made

- **Cleanup behavior:** Silent best-effort cleanup - errors ignored per CONTEXT.md decision to not mask test failures
- **VM naming:** `nbc-test-{sanitized-test-name}-{pid}` for uniqueness across parallel runs
- **Ready state:** Accept both "running" and "degraded" from systemctl (VMs may have non-critical service failures)
- **Normalization scope:** Timestamps, UUIDs, loop devices, temp paths, PIDs - covers most dynamic content

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- Minor API issue: `ContentType` field is on `StorageVolumesPost`, not `StorageVolumePut` - fixed immediately during implementation

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Test fixtures ready for use in subsequent plans
- 01-03 can use IncusFixture for VM test migration
- 01-04+ can use golden file helpers for CLI output testing
- All patterns follow existing testutil conventions (disk.go style)

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-26*
