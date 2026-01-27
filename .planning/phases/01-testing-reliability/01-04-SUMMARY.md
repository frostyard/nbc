---
phase: 01-testing-reliability
plan: 04
subsystem: testing
tags: [go, incus, vm, integration-tests, testutil]

# Dependency graph
requires:
  - phase: 01-03
    provides: "IncusFixture with snapshot and diagnostic methods"
provides:
  - "First Go-based VM test (TestIncus_Install)"
  - "test-incus-go Makefile target"
  - "TestMain with suite-level cleanup"
affects: [01-05, 01-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "VM test pattern: fixture → createVM → waitForReady → test → cleanup"
    - "TestMain for suite-level resource cleanup"

key-files:
  created:
    - pkg/incus_test.go
  modified:
    - Makefile

key-decisions:
  - "TestIncus_ prefix for Go-based Incus tests"
  - "60GB test disk matches bash test configuration"
  - "TEST_IMAGE env var for custom image override"

patterns-established:
  - "TestMain with CleanupAllNbcTestResources for clean slate"
  - "DumpDiagnostics on failure before t.Fatal"

# Metrics
duration: 1min
completed: 2026-01-26
---

# Phase 1 Plan 4: First VM Test Summary

**First Go-based VM test (TestIncus_Install) migrated from test_incus_quick.sh with fixture-based infrastructure**

## Performance

- **Duration:** 1 min
- **Started:** 2026-01-26T21:18:36Z
- **Completed:** 2026-01-26T21:19:59Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- Created `pkg/incus_test.go` with TestIncus_Install test (150 lines)
- Test validates: 4 partitions, config file, dracut module, .etc.lower directory
- Added TestMain with clean slate cleanup before and after test suite
- Added `test-incus-go` Makefile target for running Go-based VM tests

## Task Commits

Each task was committed atomically:

1. **Task 1: Create first Go-based VM test** - `6e61b20` (feat)
2. **Task 2: Add Makefile target for Go-based VM tests** - `383b14a` (feat)

## Files Created/Modified

- `pkg/incus_test.go` - First Go-based Incus VM integration test (150 lines)
- `Makefile` - Added test-incus-go target

## Decisions Made

- **TestIncus_ prefix:** All Go-based Incus tests use `TestIncus_` prefix for selective running
- **Test disk size:** 60GB matches the existing bash test configuration
- **TEST_IMAGE override:** Supports environment variable to customize test image

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None

## Next Phase Readiness

- TestIncus_Install compiles and uses proper infrastructure
- Ready for plan 01-05 to migrate remaining tests (update, boot verification)
- test-incus-go target provides entry point for Go-based VM test execution

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-26*
