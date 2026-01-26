# Roadmap: nbc SDK Extraction

## Overview

This milestone extracts a clean SDK from nbc's CLI-entangled codebase. The journey starts with test reliability (you can't refactor safely without tests), then cleans up duplicates, establishes interfaces, performs the inside-out SDK extraction, integrates logging, and finally adapts the CLI to use the new SDK. Each phase builds on the previous, with testing as the foundation that enables everything else.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Testing Reliability** - Fix flaky Incus tests to enable safe refactoring
- [ ] **Phase 2: Pre-Extraction Cleanup** - Consolidate duplicates and make paths configurable
- [ ] **Phase 3: Interface Foundation** - Import progress.Reporter and create internal/ structure
- [ ] **Phase 4: SDK Extraction** - Inside-out refactoring to extract clean SDK
- [ ] **Phase 5: Logging Integration** - Add slog-based logging separate from progress
- [ ] **Phase 6: CLI Adaptation** - Wire CLI to use new SDK with backward compatibility

## Phase Details

### Phase 1: Testing Reliability
**Goal**: Integration tests pass 100% deterministically, enabling safe refactoring
**Depends on**: Nothing (first phase)
**Requirements**: TEST-01, TEST-02, TEST-03, TEST-04, TEST-05, TEST-06, TEST-07
**Success Criteria** (what must be TRUE):
  1. `make test-integration` passes 3 consecutive runs with 0 failures
  2. Each test leaves no orphaned VMs, volumes, or mounts after completion
  3. Tests timeout cleanly with actionable error messages (no hanging)
  4. VM tests reset via snapshots between test cases (observable via test speed)
  5. CLI output changes are caught by golden file comparison tests
**Plans**: TBD

Plans:
- [ ] 01-01: TBD

### Phase 2: Pre-Extraction Cleanup
**Goal**: Codebase is clean and ready for extraction with no duplicates or hardcoded paths
**Depends on**: Phase 1
**Requirements**: (Prep work enabling SDK-01 through SDK-08)
**Success Criteria** (what must be TRUE):
  1. Zero duplicate implementations of buildKernelCmdline() or NVMe workarounds
  2. All paths that were hardcoded are now configurable via options or environment
  3. Integration tests still pass after cleanup (no regressions)
**Plans**: TBD

Plans:
- [ ] 02-01: TBD

### Phase 3: Interface Foundation
**Goal**: SDK interfaces and package structure established before implementation
**Depends on**: Phase 2
**Requirements**: SDK-06, SDK-07, SDK-08
**Success Criteria** (what must be TRUE):
  1. `github.com/frostyard/pm/progress` is imported and adapter exists
  2. `internal/` package exists with non-public implementation moved there
  3. Passing nil Reporter to any function does not panic
  4. Functional options pattern (WithLogger, WithProgress) works on at least one component
**Plans**: TBD

Plans:
- [ ] 03-01: TBD

### Phase 4: SDK Extraction
**Goal**: Clean SDK extracted with all output flowing through progress.Reporter
**Depends on**: Phase 3
**Requirements**: SDK-01, SDK-02, SDK-03, SDK-04, SDK-05
**Success Criteria** (what must be TRUE):
  1. Zero `fmt.Print*` or `os.Stdout` calls remain in pkg/ (except designated helpers)
  2. All SDK operations accept context.Context as first parameter
  3. SDK returns custom error types that work with errors.Is and errors.As
  4. Invalid configuration fails fast at construction time with clear error
  5. All exported types and functions have godoc comments (verified by linter)
**Plans**: TBD

Plans:
- [ ] 04-01: TBD

### Phase 5: Logging Integration
**Goal**: Structured slog logging to file, separate from user-facing progress
**Depends on**: Phase 4
**Requirements**: LOG-01, LOG-02, LOG-03, LOG-04, LOG-05
**Success Criteria** (what must be TRUE):
  1. SDK uses log/slog for all diagnostic logging (no fmt/log package)
  2. Logs are written to /var/log/nbc/ with rotation-friendly naming
  3. Debug slog output never appears in user-facing progress output
  4. Long operations include operation ID in log messages for correlation
  5. Sensitive data (passwords, keys, tokens) never appears in logs
**Plans**: TBD

Plans:
- [ ] 05-01: TBD

### Phase 6: CLI Adaptation
**Goal**: CLI uses SDK cleanly with full backward compatibility
**Depends on**: Phase 5
**Requirements**: CLI-01, CLI-02, CLI-03, CLI-04, CLI-05, CLI-06
**Success Criteria** (what must be TRUE):
  1. All commands use consistent flag naming (verified by audit)
  2. Error messages are human-readable and suggest what to do next
  3. `--json` flag produces consistent output across all commands
  4. Every command has help text with at least one usage example
  5. Commands print logical next steps after completion
**Plans**: TBD

Plans:
- [ ] 06-01: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Testing Reliability | 0/? | Not started | - |
| 2. Pre-Extraction Cleanup | 0/? | Not started | - |
| 3. Interface Foundation | 0/? | Not started | - |
| 4. SDK Extraction | 0/? | Not started | - |
| 5. Logging Integration | 0/? | Not started | - |
| 6. CLI Adaptation | 0/? | Not started | - |

---
*Roadmap created: 2026-01-26*
*Last updated: 2026-01-26*
