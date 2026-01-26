# Domain Pitfalls

**Domain:** Go SDK extraction from CLI, VM-based integration testing
**Researched:** 2026-01-26
**Overall confidence:** HIGH (based on codebase analysis and established patterns)

## Critical Pitfalls

Mistakes that cause rewrites or major issues.

### Pitfall 1: Extracting SDK Without Test Coverage First

**What goes wrong:** Teams extract SDK from CLI-entangled code before establishing reliable tests. The extraction introduces subtle bugs (changed default values, missing error handling paths, different context threading) that go undetected until production.

**Why it happens:** Pressure to "just refactor" and eagerness to create clean APIs. Tests seem like they slow things down.

**Consequences:**
- Regression bugs in core functionality (install fails, updates corrupt systems)
- Loss of confidence in the codebase
- Users on nbc-managed systems unable to update safely
- Potential for unbootable systems

**Warning signs:**
- Planning SDK extraction before test-integration target works reliably
- Integration tests marked "flaky" and skipped in CI
- Coverage gaps in critical paths (LUKS, bootloader, partitioning)

**Prevention:**
1. Make `make test-integration` pass 100% before any extraction work
2. Fix Incus test flakiness first (see Pitfall 4)
3. Add contract tests for SDK boundary before refactoring
4. Require coverage thresholds for extracted packages

**Phase mapping:** Testing/reliability phase MUST complete before SDK extraction phase

---

### Pitfall 2: Breaking CLI Backward Compatibility During SDK Extraction

**What goes wrong:** Extracting SDK changes how options flow through the system. CLI flag defaults change, option combinations that worked stop working, or error messages change format. Existing scripts and automation break.

**Why it happens:** Focus on "clean API design" without running the full CLI test suite. Options renamed for API clarity but CLI flags not updated.

**Consequences:**
- User automation scripts fail
- CI/CD pipelines using nbc break
- JSON output format changes break parsers
- Existing documentation becomes incorrect

**Warning signs:**
- CLI tests are 0% coverage (currently true per CONCERNS.md)
- No integration tests for flag combinations
- Planning to rename config fields for "clarity"
- No deprecation strategy for old options

**Prevention:**
1. Add CLI integration tests before extraction (test actual command invocations)
2. Create compatibility test suite that exercises documented flag combinations
3. Freeze JSON output schema during extraction (breaking changes only in major version)
4. Use adapter pattern: SDK has clean API, CLI layer translates

**Phase mapping:** CLI testing should be added in testing phase, maintained through SDK extraction

---

### Pitfall 3: Exposing Internal Types in SDK API Surface

**What goes wrong:** SDK exposes types that should be internal (progress adapters, internal configs, exec wrappers). Consumers depend on these. Later refactoring becomes impossible without breaking changes.

**Why it happens:** 
- Go's package visibility makes it easy to expose everything from `pkg/`
- Convenience of reusing internal types in public API
- "We'll clean it up later" mentality

**Consequences:**
- API surface explosion (every type becomes public contract)
- Internal refactoring blocked by external consumers
- Version churn as you try to hide previously-exposed internals
- Technical debt compounds

**Warning signs:**
- Planning to keep everything in single `pkg/` package
- Internal types like `progressAdapter` exposed in SDK
- No `internal/` package structure
- No explicit API design document

**Prevention:**
1. Create explicit public API in new `nbc/` package (or `sdk/` package)
2. Move non-public types to `internal/` package
3. Define SDK interface types separate from implementation
4. Document public API surface in code comments
5. Use interface types at API boundaries (e.g., `io.Writer` not concrete types)

**Phase mapping:** API design phase before implementation, enforced during SDK extraction

---

### Pitfall 4: Flaky VM Tests From Non-Deterministic Timing

**What goes wrong:** Incus VM tests fail randomly. VM boot takes variable time, disk attachment timing varies, systemd startup races. Tests pass locally, fail in CI.

**Why it happens:**
- Hardcoded `sleep` statements instead of proper polling
- Racing against systemd startup without proper readiness checks
- VM resource contention in CI environment
- No timeout with proper error messages

**This is already happening in nbc:**
- `test_incus.sh:124-132`: Simple timeout loop with `sleep 2`
- `test_incus_quick.sh:84`: `sleep 5` for disk recognition
- No health check after operations complete

**Consequences:**
- CI marked "flaky" and ignored
- Developers stop trusting tests
- Real failures hidden among noise
- No reliable gate for merging changes

**Warning signs:**
- `sleep` statements in test scripts
- Tests pass locally but fail in CI
- "Just rerun it" culture
- Tests marked as flaky and excluded

**Prevention:**
1. Replace all `sleep` with polling loops that check actual state
2. Add exponential backoff to polling
3. Include meaningful timeout error messages
4. Check systemd service states, not just "is-system-running"
5. Add VM health check step before any test operations
6. Parallelize tests carefully (or not at all for VM tests)

**Detection script pattern:**
```bash
# BAD: sleep 5
# GOOD: wait_for_device with actual check
wait_for_device() {
    local device=$1
    local timeout=60
    while [ $timeout -gt 0 ]; do
        if [ -b "$device" ]; then return 0; fi
        sleep 1
        timeout=$((timeout - 1))
    done
    echo "ERROR: Device $device did not appear within timeout"
    return 1
}
```

**Phase mapping:** Address in testing/reliability phase before SDK extraction

---

### Pitfall 5: Progress/Logging Abstraction That Breaks SDK Consumers

**What goes wrong:** SDK requires specific progress reporter type or logging setup. SDK consumers must match internal abstractions rather than using standard interfaces.

**This is already present in nbc:**
- `ProgressReporter` is custom type
- Dual callback-based and reporter-based systems exist
- `progressAdapter` bridges them (technical debt per CONCERNS.md)

**Consequences:**
- SDK consumers forced to implement custom interfaces
- Can't use standard `slog.Logger` or `io.Writer`
- Progress information format locked to internal design
- Breaking changes when internal progress system changes

**Warning signs:**
- Progress/logging types exported in public API
- SDK functions require specific reporter type as parameter
- No interface type for callbacks
- Mixing logging and progress in same abstraction

**Prevention:**
1. Use `github.com/frostyard/pm/progress` as planned (external, clean interface)
2. Accept `slog.Handler` or `*slog.Logger` for logging, not custom type
3. Define callback interfaces that SDK consumers implement
4. Keep output formatting in CLI layer, not SDK
5. SDK returns structured data, CLI formats for display

**Example clean interface:**
```go
// SDK accepts standard interfaces
type InstallerOption func(*Installer)

func WithLogger(l *slog.Logger) InstallerOption
func WithProgress(p progress.Handler) InstallerOption

// Callbacks use simple function types
type ProgressCallback func(step int, total int, name string)
type MessageCallback func(message string)
```

**Phase mapping:** Design phase, implement during SDK extraction

---

## Moderate Pitfalls

Mistakes that cause delays or technical debt.

### Pitfall 6: Test Fixtures Requiring Real Hardware/Root

**What goes wrong:** Tests only work with real loopback devices, root privileges, and specific tools installed. Can't run in CI, can't run on developer machines without sudo.

**Current state in nbc:**
- Integration tests require root (`testutil.RequireRoot`)
- Tests need `losetup`, `sgdisk`, `mkfs.*`, etc.
- No container-based test environment

**Prevention:**
1. Create container-based test environment (Dockerfile for test dependencies)
2. Mock filesystem operations for unit tests where possible
3. Use `testcontainers-go` for integration tests without root
4. Separate truly-hardware-dependent tests from logic tests
5. Consider fakeroot or user namespaces for some operations

**Phase mapping:** Testing phase

---

### Pitfall 7: Duplicate Logic Surviving SDK Extraction

**What goes wrong:** During extraction, duplicated code (like `buildKernelCmdline()` in both `bootloader.go` and `update.go`) gets duplicated again in SDK vs CLI, or one copy is missed.

**Current state in nbc:**
- `buildKernelCmdline()` duplicated per CONCERNS.md
- NVMe workaround duplicated in two places

**Prevention:**
1. Consolidate duplicates BEFORE extraction (not during)
2. Create single source of truth for shared logic
3. Add tests for consolidated functions
4. Extract shared utilities to internal package first

**Phase mapping:** Pre-extraction cleanup phase

---

### Pitfall 8: Hardcoded Paths Blocking Testability

**What goes wrong:** Paths like `/var/lib/nbc/state`, `/var/cache/nbc` are hardcoded. SDK consumers can't use different paths. Tests must run as root or fail to create directories.

**Current state in nbc:**
- Multiple hardcoded paths per CONCERNS.md
- `/tmp/nbc-install` as default mount point
- Lock files in `/var/run/nbc`

**Prevention:**
1. Make all paths configurable via options
2. Provide sensible defaults but allow override
3. Use `t.TempDir()` pattern in tests
4. Accept path configuration in SDK constructors
5. Consider XDG base directory spec for user-space paths

**Phase mapping:** Address during SDK extraction

---

### Pitfall 9: exec.Command Mocking Complexity

**What goes wrong:** SDK wraps 74+ external commands. Testing requires either real commands (root, slow) or complex mocking infrastructure that's fragile.

**Prevention:**
1. Create command executor interface
2. Production executor runs real commands
3. Test executor returns canned responses or records calls
4. Don't mock at syscall level—too fragile
5. Accept that some tests must run real commands (integration tier)

**Example pattern:**
```go
type CommandExecutor interface {
    Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type RealExecutor struct{}
type RecordingExecutor struct { Calls []Call }
type MockExecutor struct { Responses map[string][]byte }
```

**Phase mapping:** Consider during SDK extraction, but don't over-engineer

---

### Pitfall 10: Incus VM Cleanup Failures Leaking Resources

**What goes wrong:** Test fails mid-run, cleanup doesn't happen, VMs and storage volumes accumulate. Eventually runs out of disk or memory.

**Current mitigation in nbc:**
- `trap cleanup EXIT INT TERM` in test scripts
- Cleanup function attempts to delete VM and volume

**Still at risk:**
- Ctrl+C during incus exec may not trigger cleanup
- Storage volume deletion can fail silently
- No periodic cleanup of orphaned resources

**Prevention:**
1. Use unique prefixes with timestamps (already done: `nbc-test-$$`)
2. Add pre-test cleanup of stale resources (older than N hours)
3. Run cleanup in CI before test suite
4. Monitor disk usage in CI

**Detection:**
```bash
# Add to CI setup
incus list --format csv | grep '^nbc-test-' | while read line; do
    name=$(echo "$line" | cut -d, -f1)
    echo "Warning: Orphaned test VM found: $name"
done
```

**Phase mapping:** Testing phase

---

### Pitfall 11: Ignoring Context Cancellation in Long Operations

**What goes wrong:** User presses Ctrl+C, but operation continues. Image download keeps going. Partition creation completes even though cancelled.

**Current state in nbc:**
- Context passed to `Install()` and `Update()`
- Some context checks exist
- Not clear if all long operations respect cancellation

**Prevention:**
1. Audit all long operations for context checks
2. Check `ctx.Err()` before each major step
3. Pass context through to helper functions
4. Test cancellation behavior explicitly
5. Document which operations are cancellation-safe

**Phase mapping:** SDK extraction (clean up context threading)

---

## Minor Pitfalls

Mistakes that cause annoyance but are fixable.

### Pitfall 12: SDK Version Coupled to CLI Version

**What goes wrong:** SDK and CLI in same module, versioned together. SDK bug fix requires CLI release. Semantic versioning becomes meaningless.

**Prevention:**
1. Consider separate module for SDK (`github.com/frostyard/nbc/sdk`)
2. Or accept coupled versioning and document it
3. Use build tags if needed to separate concerns

**Phase mapping:** Post-extraction consideration

---

### Pitfall 13: Test Parallelism Causing Resource Contention

**What goes wrong:** Tests run in parallel, compete for loopback devices, mount points, or ports. Random failures.

**Current state in nbc:**
- Integration tests use unique temp dirs
- But loopback count is limited
- Tests might compete for device numbers

**Prevention:**
1. Use `t.Parallel()` carefully—only for truly independent tests
2. Serialize tests that use shared resources (loopback devices)
3. Use unique identifiers for all resources
4. Consider running integration tests with `-parallel=1`

**Phase mapping:** Testing phase

---

### Pitfall 14: Error Messages Losing Context During Extraction

**What goes wrong:** Refactoring changes error wrapping. Error messages become less helpful. "operation failed" instead of "failed to mount root partition: no such device".

**Prevention:**
1. Add error message tests (check error string contains expected context)
2. Use consistent error wrapping pattern: `fmt.Errorf("context: %w", err)`
3. Review error paths during extraction
4. Don't change error messages unless necessary

**Phase mapping:** During SDK extraction

---

### Pitfall 15: Breaking Incus Tests by Changing Output Format

**What goes wrong:** Incus tests grep/parse CLI output. Output format changes, tests fail for wrong reason.

**Current pattern in test_incus.sh:**
```bash
# Parses output to find disk
TEST_DISK=$(incus exec ... -- bash -c "lsblk -ndo ...")
```

**Prevention:**
1. Use JSON output (`--json`) for machine parsing
2. Use structured exit codes for test assertions
3. Test output format separately from functionality
4. Document output format as API contract

**Phase mapping:** Testing phase (when adding CLI tests)

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| Testing/Reliability | VM test flakiness (#4) | Replace sleep with polling, add health checks |
| Testing/Reliability | Resource cleanup (#10) | Add pre-test cleanup, monitor for orphans |
| Pre-Extraction Cleanup | Duplicate code (#7) | Consolidate before extraction |
| Pre-Extraction Cleanup | Hardcoded paths (#8) | Make configurable |
| API Design | Type exposure (#3) | Define public API explicitly, use internal/ |
| API Design | Progress abstraction (#5) | Use standard interfaces |
| SDK Extraction | CLI compatibility (#2) | Add CLI integration tests first |
| SDK Extraction | Test coverage (#1) | Coverage gate before extraction |
| SDK Extraction | Context handling (#11) | Audit and test cancellation |
| Post-Extraction | Version coupling (#12) | Decide module structure upfront |

---

## Sources

- Codebase analysis: `pkg/`, `cmd/`, `.planning/codebase/CONCERNS.md`
- Test scripts: `test_incus.sh`, `test_incus_quick.sh`
- Testing patterns: `pkg/integration_test.go`, `.planning/codebase/TESTING.md`
- Go SDK design patterns: Go standard library, established open-source SDKs
- VM testing patterns: Incus documentation, testcontainers-go patterns
