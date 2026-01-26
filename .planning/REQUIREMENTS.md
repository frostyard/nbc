# Requirements: nbc SDK Extraction

**Defined:** 2026-01-26
**Core Value:** Users can reliably install and upgrade their Linux system from OCI images with A/B partitioning for atomic updates and rollback safety.

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Testing Infrastructure

- [ ] **TEST-01**: Integration tests pass 100% deterministically on 3 consecutive CI runs
- [ ] **TEST-02**: Each test runs in isolation without affecting other tests
- [ ] **TEST-03**: Failed tests clean up all resources (VMs, volumes, mounts)
- [ ] **TEST-04**: Long-running tests have explicit timeouts with clear error messages
- [ ] **TEST-05**: Incus VM tests use Go client library instead of bash scripts
- [ ] **TEST-06**: VM tests use snapshots for fast reset between test cases
- [ ] **TEST-07**: CLI output changes detected via golden file comparison

### SDK Design

- [ ] **SDK-01**: All SDK operations accept context.Context as first parameter
- [ ] **SDK-02**: SDK defines custom error types supporting errors.Is and errors.As
- [ ] **SDK-03**: All SDK output flows through progress.Reporter interface
- [ ] **SDK-04**: SDK validates configuration at construction time, failing fast on invalid config
- [ ] **SDK-05**: All exported types and functions have godoc comments
- [ ] **SDK-06**: SDK uses functional options pattern (WithLogger, WithProgress, etc.)
- [ ] **SDK-07**: Non-public implementation lives in internal/ package
- [ ] **SDK-08**: SDK handles nil reporter gracefully without panics

### Logging

- [ ] **LOG-01**: SDK uses log/slog for all diagnostic logging
- [ ] **LOG-02**: Logs persist to /var/log/nbc/ with rotation consideration
- [ ] **LOG-03**: Debug logging (slog) is separate from user progress (Reporter)
- [ ] **LOG-04**: Long-running operations include operation IDs for log correlation
- [ ] **LOG-05**: Sensitive data (passwords, keys) is never logged

### CLI UX

- [ ] **CLI-01**: All commands use consistent flag naming conventions
- [ ] **CLI-02**: Error messages are human-readable and actionable
- [ ] **CLI-03**: Error output goes to stderr, data output to stdout
- [ ] **CLI-04**: Help text includes usage examples for common operations
- [ ] **CLI-05**: Commands suggest logical next steps after completion
- [ ] **CLI-06**: JSON output mode (--json) works consistently across all commands

## v2 Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### Testing

- **TEST-08**: Parallel VM test execution for faster CI
- **TEST-09**: Container-based test environment (no root required)
- **TEST-10**: Property-based testing for edge cases

### SDK

- **SDK-09**: SDK published as separate Go module for independent versioning
- **SDK-10**: SDK exposes test utilities package for consumer testing
- **SDK-11**: Full dry-run support for all operations

### CLI

- **CLI-07**: Colored output with TTY detection and NO_COLOR support
- **CLI-08**: Progress bars/spinners for long operations
- **CLI-09**: Interactive mode for guided configuration

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| GUI application | CLI and SDK only; consumers can build GUI |
| CGO dependencies | Must remain pure Go for cross-compilation |
| Non-Linux platforms | Linux-only by design |
| Automatic retries in SDK | Caller should control retry policy |
| Global loggers/reporters | Breaks testability; use dependency injection |
| Backward-incompatible JSON output changes | Would break existing automation |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| TEST-01 | Phase 1: Testing Reliability | Pending |
| TEST-02 | Phase 1: Testing Reliability | Pending |
| TEST-03 | Phase 1: Testing Reliability | Pending |
| TEST-04 | Phase 1: Testing Reliability | Pending |
| TEST-05 | Phase 1: Testing Reliability | Pending |
| TEST-06 | Phase 1: Testing Reliability | Pending |
| TEST-07 | Phase 1: Testing Reliability | Pending |
| SDK-01 | Phase 4: SDK Extraction | Pending |
| SDK-02 | Phase 4: SDK Extraction | Pending |
| SDK-03 | Phase 4: SDK Extraction | Pending |
| SDK-04 | Phase 4: SDK Extraction | Pending |
| SDK-05 | Phase 4: SDK Extraction | Pending |
| SDK-06 | Phase 3: Interface Foundation | Pending |
| SDK-07 | Phase 3: Interface Foundation | Pending |
| SDK-08 | Phase 3: Interface Foundation | Pending |
| LOG-01 | Phase 5: Logging Integration | Pending |
| LOG-02 | Phase 5: Logging Integration | Pending |
| LOG-03 | Phase 5: Logging Integration | Pending |
| LOG-04 | Phase 5: Logging Integration | Pending |
| LOG-05 | Phase 5: Logging Integration | Pending |
| CLI-01 | Phase 6: CLI Adaptation | Pending |
| CLI-02 | Phase 6: CLI Adaptation | Pending |
| CLI-03 | Phase 6: CLI Adaptation | Pending |
| CLI-04 | Phase 6: CLI Adaptation | Pending |
| CLI-05 | Phase 6: CLI Adaptation | Pending |
| CLI-06 | Phase 6: CLI Adaptation | Pending |

**Phase Summary:**
- Phase 1 (Testing Reliability): 7 requirements (TEST-01 through TEST-07)
- Phase 2 (Pre-Extraction Cleanup): 0 requirements (prep work, no direct requirements)
- Phase 3 (Interface Foundation): 3 requirements (SDK-06, SDK-07, SDK-08)
- Phase 4 (SDK Extraction): 5 requirements (SDK-01 through SDK-05)
- Phase 5 (Logging Integration): 5 requirements (LOG-01 through LOG-05)
- Phase 6 (CLI Adaptation): 6 requirements (CLI-01 through CLI-06)

**Coverage:**
- v1 requirements: 25 total
- Mapped to phases: 25
- Unmapped: 0 ✓

Note: Phase 2 has no direct requirements but is necessary prep work that enables
Phases 3-6. Research identified cleanup before extraction as critical (Pitfall #7).

---
*Requirements defined: 2026-01-26*
*Last updated: 2026-01-26 — Roadmap created with 6 phases*
