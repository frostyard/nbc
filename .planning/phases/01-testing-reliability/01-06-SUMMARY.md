---
phase: 01-testing-reliability
plan: 06
subsystem: testing
tags: [go-test, incus, vm-test, cleanup, finalization, reliability]

# Dependency graph
requires:
  - phase: 01-05
    provides: "Complete VM test coverage and CLI golden files"
provides:
  - "Deterministic test infrastructure with 3x consecutive pass verification"
  - "Dynamic volume naming for parallel test safety"
  - "Force flag for A/B update testing (same image scenario)"
affects: [phase-02, sdk-extraction, ci-pipeline]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "VolumeName() getter for dynamic volume access in tests"
    - "--force flag usage when testing update with identical images"
    - "Diagnostic logging in tests for debugging failures"

key-files:
  created: []
  modified:
    - "pkg/testutil/incus.go"
    - "pkg/incus_test.go"

key-decisions:
  - "Add VolumeName() getter rather than exposing volumeName field — maintains encapsulation"
  - "Use --force for update test — validates A/B mechanism even with same image digest"
  - "Keep diagnostic logging — useful for future debugging without overhead"

patterns-established:
  - "Dynamic resource naming: fixture methods return actual names for use in complex operations"
  - "Force flags in tests: explicitly test mechanisms even when conditions don't require them"

# Metrics
duration: 25min
completed: 2026-01-27
---

# Phase 01 Plan 06: Test Infrastructure Finalization

**Fixed test reliability issues and verified 3x consecutive pass**

## Performance

- **Duration:** 25 min
- **Started:** 2026-01-27T00:45:00Z
- **Completed:** 2026-01-27T01:10:00Z
- **Tasks:** 3 (Task 1-2 completed in prior session, Task 3 checkpoint completed now)
- **Files modified:** 2

## Accomplishments
- Fixed Boot test "Storage volume not found" error by using dynamic volume name
- Fixed VerifyABPartitions "root2 empty" by adding --force to update command
- Added diagnostic logging to see update output and partition contents
- Verified 3 consecutive test runs pass with no orphaned resources

## Task Commits

1. **Task 1: Update Makefile test targets** - Completed in prior session
2. **Task 2: Delete legacy bash test scripts** - Completed in prior session
3. **Task 3: Checkpoint verification** - `afaa0f3` (fix)

## Files Created/Modified
- `pkg/testutil/incus.go` - Added `VolumeName()` getter method (6 lines)
- `pkg/incus_test.go` - Fixed Boot test volume reference, added --force, added logging (20 lines changed)

## Issues Found and Fixed

### 1. Boot Test: "Storage volume not found"
- **Root cause:** `AttachDisk()` generates unique volume names (`vol-testincus-fullcy-XXXXX`) but Boot test hardcoded `"test-disk"`
- **Fix:** Added `VolumeName()` getter to IncusFixture; updated Boot test to use `fixture.VolumeName()`

### 2. VerifyABPartitions: "root2 empty after update"
- **Root cause:** `nbc update` correctly detected "already up-to-date" (same image digest) and skipped the update
- **Fix:** Added `--force` flag to update command to exercise A/B update mechanism even when digests match

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Dynamic volume name not accessible**
- **Found during:** Checkpoint debugging
- **Issue:** Boot test couldn't reference volume created by AttachDisk
- **Fix:** Added VolumeName() getter to IncusFixture
- **Files modified:** pkg/testutil/incus.go, pkg/incus_test.go

**2. [Rule 1 - Bug] Update skipped due to matching digest**
- **Found during:** Checkpoint debugging
- **Issue:** nbc update correctly skipped update (same image), breaking test assumption
- **Fix:** Added --force flag to update command in test
- **Files modified:** pkg/incus_test.go

---

**Total deviations:** 2 auto-fixed bugs
**Impact on plan:** Debugging required to identify root causes. Test infrastructure is now robust.

## Verification Results

### 3x Consecutive Pass
| Run | TestIncus_Install | TestIncus_FullCycle | Duration |
|-----|-------------------|---------------------|----------|
| 1   | PASS              | PASS                | ~7 min   |
| 2   | PASS              | PASS                | ~7 min   |
| 3   | PASS              | PASS                | ~7 min   |

### Orphaned Resource Check
```
incus list | grep nbc-test → (no results)
incus storage volume list default | grep nbc-test → (no results)
```

### Subtests Verified
- Install: Creates partitions, extracts container filesystem
- VerifyPartitions: Confirms 4-partition layout (boot, root1, root2, var)
- Update: Runs A/B update with --force, writes to root2
- VerifyABPartitions: Confirms root1 (85k files) and root2 (92k files) populated
- Boot: Creates empty VM, boots from installed disk, verifies read-only root

## Phase 1 Complete

All success criteria met:
- [x] `make test-incus` passes 3 consecutive runs with 0 failures
- [x] Each test leaves no orphaned VMs, volumes, or mounts
- [x] Tests timeout cleanly with actionable error messages
- [x] VM tests use IncusFixture with automatic cleanup
- [x] CLI output changes caught by golden file comparison
- [x] Legacy bash test scripts deleted
- [x] Makefile updated to use Go tests

## Next Steps
- Phase 2: SDK Extraction (or as defined in ROADMAP.md)
- Consider adding encryption test coverage (deferred from bash migration)
- CI integration can now use `make test-incus` reliably

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-27*
