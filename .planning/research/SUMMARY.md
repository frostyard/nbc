# Research Summary: SDK Extraction Milestone

**Synthesized:** 2026-01-26
**Sources:** ARCHITECTURE.md, FEATURES.md, PITFALLS.md
**Confidence:** HIGH

---

## Executive Summary

Extracting a clean SDK from nbc's CLI-entangled codebase requires a disciplined, phased approach. The research reveals clear patterns:

1. **Test reliability MUST precede extraction** - The single most critical pitfall is refactoring without reliable tests
2. **Inside-out refactoring** - Start with lowest-level functions, propagate Reporter interface upward
3. **Use standard interfaces** - Adopt `github.com/frostyard/pm/progress.Reporter`, accept `*slog.Logger`
4. **Explicit API boundaries** - Use `internal/` package, define public surface carefully

---

## Key Findings

### Architecture (from ARCHITECTURE.md)

**Current Problems:**
- 100+ `fmt.Print*` calls scattered across pkg/
- `ProgressReporter` contains rendering logic (text vs JSON) - should be CLI concern
- Direct `os.Stdout` references in pkg/
- Confirmation prompts (`fmt.Scanln`) in business logic
- Dual callback + reporter systems with adapter bridging them

**Target State:**
- All SDK output via `progress.Reporter` interface
- No `fmt.Print*`, `os.Stdout`, or prompts in pkg/
- Clean `Client` facade with methods: `Install()`, `Update()`, `Download()`, `Status()`, `Cache()`
- CLI layer handles rendering, prompts, and output format selection

**Refactoring Order (inside-out):**
1. Interface Foundation - import progress.Reporter
2. Low-Level Cleanup - partition.go, disk.go, config.go
3. Component Cleanup - ContainerExtractor, BootloaderInstaller, ImageCache
4. Orchestrator Cleanup - Installer, SystemUpdater
5. SDK Client API - public facade
6. CLI Adaptation - new Reporter implementations

### Features (from FEATURES.md)

**Table Stakes (must have):**
- Context-aware cancellation (already present, needs audit)
- Structured error types with `errors.Is`/`errors.As`
- Progress/event callbacks via interface
- Godoc documentation
- Deterministic test outcomes (currently flaky)
- Test isolation

**Differentiators (should have):**
- Progress interface (not struct) for consumer flexibility
- Structured logging via slog to file
- Separation: logging (debug) vs progress (UX)
- Dry-run support
- Internal/pkg separation for API boundary

**Anti-Features (must avoid):**
- Global mutable state
- Printf scattered throughout (current problem)
- Logging to stdout
- Internal types in public API
- Time-based test waits (`sleep`)

### Pitfalls (from PITFALLS.md)

**Critical (blocks success):**
1. **Extracting SDK without test coverage** - Must fix flaky tests first
2. **Breaking CLI backward compatibility** - Need CLI integration tests
3. **Exposing internal types** - Use internal/ package structure
4. **Flaky VM tests from timing** - Replace `sleep` with proper polling
5. **Progress abstraction breaking consumers** - Use standard interfaces

**Moderate (causes delays):**
- Test fixtures requiring root (container-based env needed)
- Duplicate logic surviving extraction (consolidate first)
- Hardcoded paths blocking testability (make configurable)
- exec.Command mocking complexity (interface pattern)
- Incus cleanup failures leaking resources (pre-test cleanup)
- Context cancellation ignored in long operations

---

## Phase Recommendations

Based on research synthesis, the milestone should have these phases in order:

### Phase 1: Testing Reliability
**Goal:** Incus integration tests pass 100% deterministically

**Why first:** Pitfall #1 warns against extraction without reliable tests. Current tests are flaky per CONCERNS.md.

**Key tasks:**
- Replace all `sleep` with polling loops
- Add VM health checks before test operations
- Add pre-test cleanup of orphaned resources
- Add proper timeout with error messages
- Verify test isolation

### Phase 2: Pre-Extraction Cleanup
**Goal:** Eliminate duplicate code and prepare for extraction

**Why second:** Pitfall #7 warns about duplicates surviving extraction. Clean up before moving.

**Key tasks:**
- Consolidate `buildKernelCmdline()` duplication
- Consolidate NVMe workaround duplication
- Make hardcoded paths configurable (Pitfall #8)
- Review and consolidate error handling patterns

### Phase 3: Interface Foundation
**Goal:** Establish SDK interfaces before implementation

**Why third:** Architecture research recommends defining interfaces first. Pitfall #3 warns about type exposure.

**Key tasks:**
- Add `github.com/frostyard/pm/progress` dependency
- Create adapter from current ProgressReporter to new interface
- Define public API surface (what's exported)
- Create `internal/` package structure
- Document interface design decisions

### Phase 4: SDK Extraction (Inside-Out)
**Goal:** Extract clean SDK from CLI-entangled code

**Why fourth:** This is the core work, enabled by phases 1-3.

**Sub-phases (per ARCHITECTURE.md):**
- 4a: Low-level function cleanup (partition, disk, config)
- 4b: Component cleanup (ContainerExtractor, BootloaderInstaller, ImageCache)
- 4c: Orchestrator cleanup (Installer, SystemUpdater)
- 4d: Client facade creation

**Key tasks per sub-phase:**
- Add Reporter parameter to functions
- Remove fmt.Print* calls
- Move prompts to CLI layer
- Return structured results
- Maintain context threading

### Phase 5: Logging Integration
**Goal:** Structured logging separate from progress

**Why fifth:** Builds on clean SDK. Features research identifies logging vs progress separation as differentiator.

**Key tasks:**
- Add slog integration
- Configure log file output (/var/log/nbc/)
- Add operation IDs for correlation
- Ensure sensitive data redaction

### Phase 6: CLI Adaptation
**Goal:** CLI uses SDK cleanly

**Why last:** CLI is the consumer of SDK.

**Key tasks:**
- Create TextReporter and JSONReporter implementations
- Handle prompts in CLI layer
- Verify backward compatibility (all flag combinations)
- Remove deprecated ProgressReporter usage
- Add CLI integration tests

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Flaky tests mask regressions | High | Critical | Phase 1 fixes this first |
| API breaks for JSON consumers | Medium | High | Freeze JSON schema, test explicitly |
| Extraction introduces bugs | Medium | High | Reliable tests + incremental phases |
| Over-engineering interfaces | Medium | Medium | Start simple, iterate |
| Timeline slip | Medium | Medium | Phases are independently valuable |

---

## Success Criteria

**Milestone complete when:**
1. `make test-integration` passes 100% on 3 consecutive runs
2. SDK can be used from external package (import test)
3. No `fmt.Print*` or `os.Stdout` in pkg/ (except designated CLI helpers)
4. All prompts happen in cmd/ layer
5. `github.com/frostyard/pm/progress.Reporter` used throughout
6. slog logging to file works
7. Existing CLI functionality unchanged (backward compatible)

---

## Effort Estimate

| Phase | Complexity | Estimated Effort |
|-------|------------|------------------|
| 1. Testing Reliability | High | 1-2 weeks |
| 2. Pre-Extraction Cleanup | Low-Medium | 3-5 days |
| 3. Interface Foundation | Medium | 3-5 days |
| 4. SDK Extraction | High | 2-3 weeks |
| 5. Logging Integration | Medium | 3-5 days |
| 6. CLI Adaptation | Medium | 1 week |

**Total:** 6-8 weeks

---

## Immediate Next Actions

1. Create milestone roadmap with phase structure above
2. Start Phase 1: Fix test_incus.sh flakiness
3. Establish CI gate requiring 100% test pass

---

*Research synthesis complete. Ready for roadmap creation.*
