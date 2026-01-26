---
phase: 01-testing-reliability
plan: 01
subsystem: testing
tags: [incus, goldie, go-testing, integration-tests]

# Dependency graph
requires: []
provides:
  - Incus Go client for programmatic VM management
  - Goldie library for golden file testing
  - Timeout constants for deterministic test execution
affects: [01-02, 01-03, 01-04, 01-05, 01-06]

# Tech tracking
tech-stack:
  added:
    - github.com/lxc/incus/v6 v6.21.0
    - github.com/sebdah/goldie/v2 v2.8.0
  patterns:
    - Structured test timeouts per test type

key-files:
  created:
    - pkg/testutil/deps.go
    - pkg/testutil/timeouts.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "Use incus/v6 client for VM operations (official Go client with proper API access)"
  - "Use goldie/v2 for golden file testing (mature, -update flag, colored diffs)"

patterns-established:
  - "Timeout constants: TimeoutUnit (30s), TimeoutIntegration (2m), TimeoutVM (10m)"

# Metrics
duration: 1 min
completed: 2026-01-26
---

# Phase 01 Plan 01: Add Dependencies and Timeout Constants Summary

**Incus Go client v6.21.0 and goldie v2.8.0 added with structured timeout constants for unit, integration, and VM tests**

## Performance

- **Duration:** 1 min
- **Started:** 2026-01-26T21:06:22Z
- **Completed:** 2026-01-26T21:07:39Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- Added Incus Go client library (github.com/lxc/incus/v6 v6.21.0) for programmatic VM management
- Added goldie golden file testing library (github.com/sebdah/goldie/v2 v2.8.0) for CLI output comparison
- Created structured timeout constants for different test types (unit: 30s, integration: 2m, VM: 10m)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add test infrastructure dependencies** - `584ce92` (chore)
2. **Task 2: Create timeout constants** - `68eb34d` (feat)

## Files Created/Modified

- `go.mod` - Added Incus and goldie dependencies
- `go.sum` - Updated dependency checksums
- `pkg/testutil/deps.go` - Dependency imports to track in go.mod
- `pkg/testutil/timeouts.go` - Timeout constants for all test types

## Decisions Made

- Used github.com/lxc/incus/v6 (official Go client) over shell exec for Incus operations
- Used goldie/v2 over charmbracelet/x/exp/golden (more features: JSON/XML, templates, colored diffs)
- Created deps.go to ensure dependencies stay in go.mod (blank imports prevent go mod tidy from removing unused deps)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Incus client and goldie are available for import
- Timeout constants defined and ready for use
- Ready for 01-02-PLAN.md (Create Incus fixture and golden file helpers)

---
*Phase: 01-testing-reliability*
*Completed: 2026-01-26*
