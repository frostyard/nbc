---
phase: 01-testing-reliability
plan: 05
subsystem: testing
tags: [go-test, incus, vm-test, golden-files, goldie, boot-test, a-b-update]

# Dependency graph
requires:
  - phase: 01-04
    provides: "IncusFixture with snapshot reset, first TestIncus_Install test"
provides:
  - "Complete VM test coverage for install/update/boot"
  - "CLI golden file tests for help output regression"
  - "TestIncus_FullCycle with A/B update and boot verification"
affects: [phase-02, sdk-extraction, ci-pipeline]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Subtest structure for multi-phase tests (install -> update -> boot)"
    - "Boot test using separate VM with disk as boot device"
    - "Golden file tests with version normalization"

key-files:
  created:
    - "pkg/cli_test.go"
    - "pkg/testdata/*.golden (7 files)"
  modified:
    - "pkg/incus_test.go"
    - "pkg/testutil/golden.go"

key-decisions:
  - "Single TestIncus_FullCycle vs separate tests — combined for CI efficiency"
  - "Boot test creates separate empty VM — avoids modifying test environment"
  - "Version string normalization in golden files — dynamic content made deterministic"

patterns-established:
  - "bootTestFixture helper: Manages boot test VM lifecycle with proper cleanup"
  - "Golden file tests run with nbc binary built at project root"
  - "Version regex: v0.14.0-25-g5568b48 -> VERSION for stable comparisons"

# Metrics
duration: 12min
completed: 2026-01-26
---

# Phase 01 Plan 05: VM Test Migration Complete

**Full VM test cycle (install → update → boot) plus CLI golden file tests with deterministic output normalization**

## Performance

- **Duration:** 12 min
- **Started:** 2026-01-26T21:20:00Z
- **Completed:** 2026-01-26T21:32:00Z
- **Tasks:** 3
- **Files modified:** 9

## Accomplishments
- TestIncus_FullCycle covers complete install → verify → update → verify → boot → verify flow
- Boot test creates empty VM, attaches installed disk, verifies /etc overlay and read-only root
- 7 CLI golden files capture help output for regression detection
- Version normalization ensures golden tests pass regardless of build version

## Task Commits

Each task was committed atomically:

1. **Task 1: Add A/B update and boot tests** - `6a97372` (feat)
2. **Task 2: Create CLI golden file tests** - `5568b48` (feat)
3. **Task 3: Generate initial golden files** - `6246308` (test)

## Files Created/Modified
- `pkg/incus_test.go` - Extended with TestIncus_FullCycle (595 lines total)
- `pkg/cli_test.go` - New file with 7 CLI golden file tests
- `pkg/testutil/golden.go` - Added version string normalization
- `pkg/testdata/help.golden` - Main help output (30 lines)
- `pkg/testdata/install-help.golden` - Install subcommand help (61 lines)
- `pkg/testdata/list-help.golden` - List subcommand help (14 lines)
- `pkg/testdata/status-help.golden` - Status subcommand help (30 lines)
- `pkg/testdata/update-help.golden` - Update subcommand help (55 lines)
- `pkg/testdata/validate-help.golden` - Validate subcommand help (15 lines)
- `pkg/testdata/version.golden` - Version output (1 line, normalized)

## Decisions Made
- **Single FullCycle test vs separate tests**: Combined install/update/boot into single test function with subtests for CI efficiency (avoids multiple VM creations)
- **Boot test VM approach**: Creates separate empty VM with installed disk as boot device rather than modifying existing test VM
- **Version normalization**: Added regex `v\d+\.\d+\.\d+(-\d+-g[a-f0-9]+)?` -> `VERSION` to handle dynamic version strings in golden files

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed regex incompatibility in golden.go**
- **Found during:** Task 3 (Generate golden files)
- **Issue:** Go regexp doesn't support negative lookahead `(?!)` - tests failed
- **Fix:** Replaced with compatible date matching pattern
- **Files modified:** pkg/testutil/golden.go
- **Committed in:** 6246308

**2. [Rule 1 - Bug] Fixed version command in CLI test**
- **Found during:** Task 3 (Generate golden files)
- **Issue:** `nbc version` is not a valid command (it's `--version` flag)
- **Fix:** Changed to `exec.Command(nbcPath(t), "--version")`
- **Files modified:** pkg/cli_test.go
- **Committed in:** 6246308

---

**Total deviations:** 2 auto-fixed (2 bugs)
**Impact on plan:** Minor fixes to ensure tests work correctly. No scope creep.

## Issues Encountered
- **Pre-existing TestImageCache_* failures**: Lock file `/var/run/nbc/cache.lock` requires root permissions. These tests fail without sudo but are not related to this plan's work. Should be addressed in a future plan.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- All VM tests from test_incus.sh migrated to Go
- Golden file infrastructure ready for CLI output regression testing
- Test suite ready for CI integration (phase 01-06)
- Pre-existing cache test permission issue should be addressed

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-26*
