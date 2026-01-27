# Phase 1: Testing Reliability - Context

**Gathered:** 2026-01-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix flaky Incus tests so integration tests pass 100% deterministically. This enables safe SDK refactoring in subsequent phases. Scope includes test infrastructure, fixtures, cleanup, and golden file comparison — not new test coverage or features.

</domain>

<decisions>
## Implementation Decisions

### Test failure reporting
- Verbose diagnostic dump on failure: last 50 lines of VM output, mounted volumes, network state
- Output goes both inline (summary) and to separate log files (test-failures/{test-name}_{timestamp}.log)
- Timeout messages include reason and last action: "Test timed out after 60s waiting for VM boot"

### Snapshot reset strategy
- Reset VM to snapshot before every test function (not per-suite)
- Fresh state for each test ensures isolation and determinism

### Golden file workflow
- Use -update flag to regenerate golden files when output changes intentionally
- Normalize dynamic content (timestamps, UUIDs, paths) before comparison

### Cleanup behavior
- Silent cleanup of orphaned resources (no fail, no warn)
- Both per-test cleanup (defer/teardown) and suite-level fallback cleanup
- Clean slate on start: delete all nbc-test-* resources before running tests

### Claude's Discretion
- Timeout values per test type (VM tests vs unit tests)
- Snapshot sharing strategy (shared baseline vs per-test snapshots)
- Snapshot naming convention
- Whether VM tests run parallel or sequential
- Diff format for golden file mismatches
- Golden file storage location (testdata/ or central)
- Graceful vs force shutdown during cleanup

</decisions>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 01-testing-reliability*
*Context gathered: 2026-01-26*
