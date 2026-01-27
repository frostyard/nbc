# Milestone: SDK Extraction

**Created:** 2026-01-26
**Status:** Planning
**Target:** Clean SDK with progress.Reporter, structured logging, reliable tests

---

## Goal

Extract a reusable SDK from nbc's CLI-entangled codebase so that:
1. External Go programs can use nbc as a library
2. All output flows through `github.com/frostyard/pm/progress.Reporter`
3. Structured logging via slog to file (separate from user progress)
4. Reliable integration tests gate all changes

---

## Phases

### Phase 1: Testing Reliability
**Goal:** Incus integration tests pass 100% deterministically

**Rationale:** Cannot safely refactor without reliable tests. Current tests are flaky due to timing issues.

**Scope:**
- Fix test_incus.sh and test_incus_quick.sh timing issues
- Replace `sleep` statements with polling loops
- Add VM health checks before test operations
- Add pre-test cleanup of orphaned VMs/volumes
- Verify tests pass 3 consecutive times in CI

**Success Criteria:**
- `make test-integration` passes 100% on 3 consecutive CI runs
- No `sleep` statements in test scripts (replaced with wait_for_* functions)
- Orphan detection/cleanup added to CI

**Estimated Effort:** 1-2 weeks

---

### Phase 2: Pre-Extraction Cleanup
**Goal:** Consolidate duplicates and prepare codebase for extraction

**Rationale:** Duplicate code and hardcoded paths will cause problems during extraction. Clean up first.

**Scope:**
- Consolidate `buildKernelCmdline()` (duplicated in bootloader.go and update.go)
- Consolidate NVMe workaround (duplicated in two places)
- Make hardcoded paths configurable (/var/lib/nbc/state, /var/cache/nbc, etc.)
- Review error handling patterns for consistency

**Success Criteria:**
- No duplicate business logic across files
- All paths configurable via options (with sensible defaults)
- Error wrapping follows consistent pattern

**Estimated Effort:** 3-5 days

---

### Phase 3: Interface Foundation
**Goal:** Establish SDK interfaces and package structure

**Rationale:** Define API surface before implementation to avoid exposing internal types.

**Scope:**
- Add `github.com/frostyard/pm/progress` dependency
- Create adapter: current ProgressReporter → progress.Reporter
- Create `internal/` package for non-public code
- Define public API types (InstallConfig, UpdateConfig, Result types)
- Document interface design decisions

**Success Criteria:**
- progress.Reporter available for use
- `internal/` package structure in place
- Public API surface documented in code comments
- Adapter allows incremental migration (old code still works)

**Estimated Effort:** 3-5 days

---

### Phase 4: SDK Extraction
**Goal:** Migrate all pkg/ code to use progress.Reporter, remove fmt.Print entanglement

**Rationale:** Core extraction work, enabled by phases 1-3.

**Sub-phases:**

#### 4a: Low-Level Function Cleanup
**Files:** partition.go, disk.go, config.go, dracut.go, fstab.go
**Tasks:**
- Add Reporter parameter to functions
- Replace fmt.Print* with reporter.OnMessage/OnStep
- Ensure context threading

#### 4b: Component Cleanup
**Files:** container.go, bootloader.go, cache.go, luks.go
**Tasks:**
- Refactor ContainerExtractor.Extract to use Reporter
- Refactor BootloaderInstaller.Install to use Reporter
- Refactor ImageCache.Download to use Reporter
- Remove stored Progress fields

#### 4c: Orchestrator Cleanup
**Files:** install.go, update.go
**Tasks:**
- Replace InstallCallbacks with single Reporter
- Replace SystemUpdater.Progress field with parameter
- Move confirmation prompts to CLI layer
- Remove callback adapters

#### 4d: Client Facade
**Files:** New pkg/client.go
**Tasks:**
- Create `type Client struct` as public facade
- Expose: Install, Update, Download, Status, Cache methods
- Hide internal types (unexported)
- Add godoc examples

**Success Criteria:**
- Zero `fmt.Print*` calls in pkg/ (outside designated helpers)
- Zero `os.Stdout` references in pkg/
- Zero user prompts in pkg/
- All operations accept progress.Reporter parameter
- Client facade usable from external package

**Estimated Effort:** 2-3 weeks

---

### Phase 5: Logging Integration
**Goal:** Structured logging separate from progress output

**Rationale:** Debug logs to file, progress to user - clean separation.

**Scope:**
- Add slog integration throughout SDK
- Configure log file output (/var/log/nbc/)
- Add operation IDs for log correlation
- Ensure sensitive data not logged (passwords, keys)
- Accept *slog.Logger in SDK config

**Success Criteria:**
- slog used for all debug/diagnostic output
- Log file created during operations
- Logs include operation correlation IDs
- No sensitive data in logs

**Estimated Effort:** 3-5 days

---

### Phase 6: CLI Adaptation
**Goal:** CLI uses SDK as intended consumer

**Rationale:** CLI demonstrates proper SDK usage, validates API design.

**Scope:**
- Create TextReporter implementation (terminal output)
- Create JSONReporter implementation (--json mode)
- Handle all prompts in cmd/ layer
- Verify all existing flag combinations still work
- Remove deprecated ProgressReporter usage
- Add CLI integration tests for key workflows

**Success Criteria:**
- All existing CLI functionality works unchanged
- JSON output format unchanged (backward compatible)
- No ProgressReporter usage (fully migrated to progress.Reporter)
- CLI integration tests pass

**Estimated Effort:** 1 week

---

## Dependencies

```
Phase 1 ──────────────────────────────────────────────────────────────►
         │
Phase 2  └──► (can start after Phase 1 begins showing progress)
              │
Phase 3       └──► (after Phase 2 complete)
                   │
Phase 4            └──► (after Phase 3 complete)
                        │
Phase 5                 └──► (after Phase 4 complete)
                             │
Phase 6                      └──► (after Phase 4 complete, parallel with 5)
```

Notes:
- Phase 1 is foundation - must complete before major refactoring
- Phases 5 and 6 can run in parallel after Phase 4
- Each phase delivers independently valuable improvements

---

## Risks

| Risk | Mitigation |
|------|------------|
| Test fixes take longer than expected | Timebox to 2 weeks, accept some flakiness if needed |
| API design wrong for consumers | Review with potential SDK users early |
| Backward compatibility breaks | CLI integration tests catch regressions |
| Scope creep into "nice to have" | Strict phase scoping, defer to future milestone |

---

## Out of Scope

- Color output / TUI improvements (future milestone)
- Parallel VM test execution (future milestone)
- CGO-based optimizations (explicitly out of scope per PROJECT.md)
- New features (this is refactoring, not feature work)

---

## Files to Modify

**Phase 1:**
- test_incus.sh
- test_incus_quick.sh
- Makefile (CI targets)

**Phase 2:**
- pkg/bootloader.go
- pkg/update.go
- pkg/config.go (path configuration)

**Phase 3:**
- go.mod (new dependency)
- pkg/progress.go (adapter)
- New: internal/ package structure

**Phase 4:**
- pkg/partition.go, pkg/disk.go, pkg/config.go
- pkg/container.go, pkg/bootloader.go, pkg/cache.go, pkg/luks.go
- pkg/install.go, pkg/update.go
- New: pkg/client.go

**Phase 5:**
- pkg/*.go (slog integration)

**Phase 6:**
- cmd/*.go (all commands)
- pkg/progress.go (deprecated, remove)

---

*Roadmap ready. Start with Phase 1: Testing Reliability.*
