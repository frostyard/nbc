---
phase: 01-testing-reliability
plan: 03
subsystem: testing
tags: [incus, cleanup, snapshots, diagnostics, integration-testing]

requires:
  - phase: 01-02
    provides: "IncusFixture with VM management and golden file testing"
provides:
  - "Suite-level cleanup utilities (CleanupOrphanedResources, CleanupAllNbcTestResources)"
  - "Snapshot management (CreateBaselineSnapshot, ResetToSnapshot)"
  - "Diagnostic dumping on test failure (DumpDiagnostics)"
  - "Actionable timeout errors with duration and last action"
affects: [01-04, 01-05, 01-06]

tech-stack:
  added: []
  patterns:
    - "Silent cleanup that never fails tests"
    - "Diagnostic dumps to test-failures/ on failure"
    - "Per-test snapshot reset for isolation"

key-files:
  created:
    - pkg/testutil/cleanup.go
  modified:
    - pkg/testutil/incus.go
    - .gitignore

key-decisions:
  - "Cleanup errors are silently ignored to not mask test failures"
  - "Diagnostics capture console (journalctl), mounts (findmnt), network (ip addr)"
  - "ResetToSnapshot includes WaitForReady for complete reset cycle"

patterns-established:
  - "TestMain pattern: cleanup before and after test run"
  - "Diagnostic dump on timeout with reason, location, and duration"

duration: 2 min
completed: 2026-01-26
---

# Phase 1 Plan 3: Cleanup and Diagnostics Summary

**Suite-level cleanup utilities, snapshot reset for test isolation, and diagnostic dumping for failure analysis**

## Performance

- **Duration:** 2 min
- **Started:** 2026-01-26T21:14:16Z
- **Completed:** 2026-01-26T21:16:19Z
- **Tasks:** 3
- **Files modified:** 3

## Accomplishments

- Added snapshot management methods (CreateBaselineSnapshot, ResetToSnapshot) to IncusFixture
- Created cleanup.go with suite-level resource cleanup utilities
- Added DumpDiagnostics for capturing VM state on test failure
- Improved WaitForReady timeout errors to include duration and last action
- Added test-failures/ to .gitignore for diagnostic logs

## Task Commits

Each task was committed atomically:

1. **Task 1: Add snapshot and diagnostic methods to IncusFixture** - `ede0cb0` (feat)
2. **Task 2: Create suite-level cleanup utilities** - `b34c381` (feat)
3. **Task 3: Add test-failures to .gitignore** - `783a089` (chore)

## Files Created/Modified

- `pkg/testutil/incus.go` - Added CreateBaselineSnapshot, ResetToSnapshot, DumpDiagnostics methods; improved WaitForReady timeout messages (555 lines)
- `pkg/testutil/cleanup.go` - New file with CleanupOrphanedResources, CleanupAllNbcTestResources, CleanupOrphanedMounts (142 lines)
- `.gitignore` - Added test-failures/ directory

## Decisions Made

- **Cleanup is silent**: All cleanup errors are ignored to avoid masking actual test failures
- **Journalctl for console output**: Using `journalctl -n 50` instead of GetConsole API (more reliable across distros)
- **ResetToSnapshot includes ready wait**: Combines RestoreSnapshot + WaitForReady for complete reset cycle

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Cleanup utilities ready for TestMain integration
- Snapshot methods ready for per-test reset pattern
- Diagnostic dumping ready for failure analysis
- Ready for 01-04-PLAN.md

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-26*
