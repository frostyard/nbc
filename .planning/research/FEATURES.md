# Feature Landscape: Go SDK Extraction & Testing Infrastructure

**Domain:** Go SDK design, VM-based integration testing, CLI UX
**Researched:** 2026-01-26
**Overall Confidence:** HIGH (Go official docs, established community patterns, clig.dev)

---

## Table Stakes

Features users expect. Missing = SDK feels incomplete or unusable.

### SDK Design

| Feature | Why Expected | Complexity | Dependencies | Notes |
|---------|--------------|------------|--------------|-------|
| **Context-aware cancellation** | Standard Go pattern; operations must be interruptible | Low | None | All long-running ops accept `context.Context` as first param |
| **Structured error types** | Callers need to distinguish error kinds programmatically | Medium | None | Use custom error types with `errors.Is`/`errors.As` support |
| **Config validation at construction** | Fail fast on invalid config; don't start then fail | Low | None | `Validate()` method + early validation in constructors |
| **Progress/event callbacks** | Long operations need progress reporting for UX | Medium | Interface design | Interface injection, not concrete types |
| **Sensible defaults** | Zero-config should work for common cases | Low | None | Config structs with default application in New() |
| **Explicit cleanup semantics** | Resources must be releasable; Go has no RAII | Low | None | Return cleanup funcs or implement io.Closer |
| **Godoc documentation** | Packages must be discoverable on pkg.go.dev | Low | None | Doc comments on all exported types/funcs |
| **Semantic versioning** | Consumers need version stability guarantees | Low | None | Follow go modules semver |
| **No panics crossing API boundary** | SDKs must not crash caller's program | Low | None | Recover in public entry points, return errors |

### Testing Infrastructure

| Feature | Why Expected | Complexity | Dependencies | Notes |
|---------|--------------|------------|--------------|-------|
| **Deterministic test outcomes** | Flaky tests erode confidence and slow development | High | Proper isolation | Retry logic is bandaid; fix root causes |
| **Test isolation** | Tests must not affect each other | Medium | VM/container management | Each test gets fresh environment |
| **Cleanup on failure** | Failed tests must not leak resources | Medium | Proper defer patterns | Track resources created, cleanup in all paths |
| **Timeout handling** | Hung tests must eventually fail, not hang CI | Low | Context with timeout | Pass context through all layers |
| **Skip when prerequisites missing** | Tests requiring root/tools should skip gracefully | Low | None | `t.Skip()` with clear reason |
| **Table-driven test patterns** | Reduces boilerplate, improves coverage visibility | Low | None | Standard Go testing pattern |

### CLI UX

| Feature | Why Expected | Complexity | Dependencies | Notes |
|---------|--------------|------------|--------------|-------|
| **`-h`/`--help` flags** | Universal expectation; frustrating when missing | Low | Cobra (exists) | Already present |
| **Non-zero exit on error** | Scripts depend on this for error detection | Low | None | Already present |
| **Error messages to stderr** | stdout for data, stderr for diagnostics | Low | None | Audit current usage |
| **Human-readable errors** | Raw stack traces alienate users | Medium | Error wrapping | Wrap with context, show actionable messages |
| **Consistent flag naming** | Users shouldn't guess; `-v` vs `--verbose` etc | Low | None | Audit for consistency |
| **Confirmation for destructive ops** | Disk wiping needs explicit consent | Low | None | Appears present |

---

## Differentiators

Features that set the SDK/tool apart. Not expected, but valued.

### SDK Design

| Feature | Value Proposition | Complexity | Dependencies | Notes |
|---------|-------------------|------------|--------------|-------|
| **Progress interface (not struct)** | Consumers can inject any progress impl (TUI, JSON, logging) | Medium | Interface definition | `Reporter` interface with Step/Progress/Message/etc |
| **Structured logging via slog** | Modern Go 1.21+ pattern; integrates with ecosystem | Medium | log/slog | Accept `*slog.Logger` in config, use throughout |
| **Functional options pattern** | Cleaner API for optional config | Low | None | `WithLogger()`, `WithProgress()` pattern |
| **Dry-run support in SDK** | Test what would happen without side effects | Medium | Implementation in each op | Flag propagated through config |
| **Operation result types** | Rich return values beyond just error | Low | None | `InstallResult` pattern already exists |
| **Example tests in godoc** | Runnable examples in documentation | Low | None | `Example...` functions in `_test.go` |
| **Internal/pkg separation** | Clear public API boundary | Low | Package restructure | `/pkg` for public, `/internal` for private |
| **Zero external dependencies for core** | Reduces version conflicts for consumers | High | Careful design | May not be achievable given domain |
| **Testable package (`testing.go`)** | Helpers for consumers to test their code using SDK | Medium | Test utilities | Mocks, stubs, test helpers |

### Testing Infrastructure

| Feature | Value Proposition | Complexity | Dependencies | Notes |
|---------|-------------------|------------|--------------|-------|
| **VM-based integration tests** | True isolation; tests root ops safely | High | Incus/LXD | Already started; needs reliability work |
| **Snapshot-based test reset** | Fast test setup via VM snapshots | Medium | Incus API | Faster than recreating VMs |
| **Parallel test execution** | Faster CI; each test in isolated VM | High | VM pool management | Requires careful resource management |
| **Golden file testing** | Detect output regressions automatically | Low | testdata directory | Store expected outputs, compare |
| **Test fixtures as code** | Reproducible test environments | Medium | Scripted VM setup | Declarative test prereqs |
| **slogtest for handler verification** | Verify custom log handlers work correctly | Low | testing/slogtest | Standard library package |

### CLI UX

| Feature | Value Proposition | Complexity | Dependencies | Notes |
|---------|-------------------|------------|--------------|-------|
| **Suggest next commands** | Guide users through workflows | Medium | Context-aware logic | "Next: run `nbc status` to verify" |
| **Colored output (TTY-aware)** | Better scannability, modern feel | Low | go-isatty or similar | Respect NO_COLOR env var |
| **Progress bars/spinners** | Visual feedback for long operations | Medium | Progress interface | Only in TTY mode |
| **JSON output mode (`--json`)** | Machine-readable for scripting/automation | Low | Already exists | Verify consistency across commands |
| **Command suggestions on typo** | Helpful error correction | Low | Cobra built-in | Already available |
| **Examples in help text** | Users learn from examples faster than descriptions | Low | Cobra annotations | Add common use cases |
| **Dry-run previews** | Show what would happen before destructive ops | Medium | SDK dry-run support | Display planned changes |
| **Structured log file output** | Debug issues without re-running | Medium | slog + file handler | Log to `/var/log/nbc/` |

### Logging Architecture

| Feature | Value Proposition | Complexity | Dependencies | Notes |
|---------|-------------------|------------|--------------|-------|
| **Separation: logging vs progress** | Different purposes; logging for debug, progress for UX | Medium | Clear interfaces | Log: debug info to file; Progress: user feedback |
| **Log levels in file, not terminal** | Terminal stays clean; log file has detail | Low | slog handlers | TextHandler to file, custom to terminal |
| **Request/operation IDs** | Correlate logs across complex operations | Low | slog.With() | Add operation ID at start |
| **Redaction of sensitive data** | Don't log passwords/keys | Low | slog.LogValuer | Custom type for sensitive strings |

---

## Anti-Features

Features to explicitly NOT build. Common mistakes in this domain.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| **Global mutable state** | Breaks testability, causes race conditions | Inject dependencies; use struct fields |
| **Printf scattered throughout** | Mixes concerns; can't switch output format | Use progress interface or structured logging |
| **Automatic retries in SDK** | Caller should decide retry policy | Return clear errors; let caller retry |
| **Hidden network calls** | Surprising side effects break trust | Document all network operations; consider requiring explicit enablement |
| **Magic environment variable reading** | Implicit config is confusing | Accept config explicitly; document env var usage if used |
| **CLI-specific logic in SDK** | Breaks reuse; SDK shouldn't know about Cobra | Pass config structs, not viper or flag values |
| **Swallowing errors** | Hides problems; debugging nightmare | Return all errors; use warnings for non-fatal issues |
| **Logging to stdout** | Pollutes data stream; breaks pipes | Logs to stderr or file; stdout for data only |
| **Panic on recoverable errors** | Crashes caller's program | Return errors; panic only for programmer bugs |
| **God objects** | `Installer` doing everything is hard to test | Compose smaller, focused components |
| **Internal types in public API** | Locks you into implementation details | Use interfaces at boundaries |
| **Time-based test waits** | Flaky, slow; will eventually fail | Use proper synchronization: polls, channels, conditions |
| **Test skip without reason** | CI passes but coverage is fake | Log why skip; track skip reasons |

---

## Feature Dependencies

```
                     Context Support
                           |
                           v
              +--- Config Validation ---+
              |                         |
              v                         v
       Error Types              Progress Interface
              |                         |
              +------------+------------+
                           |
                           v
                    SDK Core APIs
                    (Install, Update, etc.)
                           |
              +------------+------------+
              |                         |
              v                         v
        CLI Commands              Logging (slog)
              |                         |
              v                         v
        User-facing UX          Log File Output
```

**Dependency Chain:**

1. **Context support** - Foundation for cancellation (already done)
2. **Config validation** - Must precede operations
3. **Error types + Progress interface** - Define these before SDK work
4. **SDK core APIs** - Depend on above
5. **CLI + Logging** - Consume SDK APIs

---

## MVP Recommendation

For the SDK extraction milestone, prioritize:

### Must Have (Table Stakes)
1. **Progress interface definition** - Foundation for all output unification
2. **Error type hierarchy** - Distinguish user errors vs system errors
3. **slog integration** - Replace scattered logging
4. **Reliable VM test infrastructure** - Tests must be trustworthy

### Should Have (High-Value Differentiators)
5. **Internal/pkg split** - Clean API boundary
6. **Log file output** - Debug production issues
7. **Consistent CLI flags** - Audit and fix inconsistencies

### Defer to Later
- Parallel VM testing (complex orchestration)
- Zero external dependencies (not achievable for this domain)
- Full dry-run for all operations (incremental work)
- Color output (nice-to-have polish)

---

## Complexity Estimates

| Feature Category | Low | Medium | High |
|------------------|-----|--------|------|
| SDK Design | 6 | 4 | 1 |
| Testing Infrastructure | 3 | 3 | 2 |
| CLI UX | 6 | 4 | 0 |
| Logging | 3 | 2 | 0 |

**Total Effort Distribution:**
- Low complexity: 18 features (~1-2 days each)
- Medium complexity: 13 features (~3-5 days each)  
- High complexity: 3 features (~1-2 weeks each)

---

## Sources

**HIGH Confidence (Official Documentation):**
- [Go Blog: Structured Logging with slog](https://go.dev/blog/slog) - slog design rationale
- [Effective Go](https://go.dev/doc/effective_go) - naming, interface design
- [Go Blog: Package Names](https://go.dev/blog/package-names) - API naming conventions
- [Go Modules Layout](https://go.dev/doc/modules/layout) - cmd/internal/pkg patterns

**MEDIUM Confidence (Verified Community Standards):**
- [golang-standards/project-layout](https://github.com/golang-standards/project-layout) - directory structure
- [CLI Guidelines (clig.dev)](https://clig.dev/) - comprehensive CLI UX patterns
- [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) - interface-based plugin design

**Domain Knowledge (Existing Codebase):**
- Current `pkg/install.go` - existing callback pattern (`InstallCallbacks`)
- Current `pkg/progress.go` - existing progress reporter
- Current `cmd/*.go` - CLI structure with Cobra
