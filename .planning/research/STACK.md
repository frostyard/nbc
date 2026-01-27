# Technology Stack Recommendations: SDK Extraction Milestone

**Researched:** 2026-01-26
**Domain:** Go SDK extraction, VM-based testing, CLI UX
**Overall Confidence:** HIGH

---

## Recommended Stack

### SDK Design

| Component | Recommendation | Version | Confidence | Rationale |
|-----------|---------------|---------|------------|-----------|
| **Progress Interface** | `github.com/frostyard/pm/progress` | v0.2.1 | HIGH | Owned library, clean Reporter interface with Action/Task/Step hierarchy |
| **Logging** | `log/slog` (stdlib) | Go 1.24 | HIGH | Standard library since Go 1.21, structured logging, ecosystem integration |
| **Functional Options** | Standard pattern | n/a | HIGH | Clean API for optional configuration: `WithLogger()`, `WithProgress()` |
| **Context** | `context.Context` (stdlib) | Go 1.24 | HIGH | Already present, ensure consistent threading through all operations |

### Testing Infrastructure

| Component | Recommendation | Version | Confidence | Rationale |
|-----------|---------------|---------|------------|-----------|
| **Test Framework** | `testing` (stdlib) | Go 1.24 | HIGH | Consistency with existing codebase, no migration needed |
| **Assertions** | `github.com/stretchr/testify` | v1.11.1 | HIGH | Industry standard, require/assert packages, works with stdlib |
| **Mocking** | `go.uber.org/mock` | v0.6.0 | MEDIUM | Mockgen for interface mocking, lighter than alternatives |
| **VM Testing** | `github.com/lxc/incus/client` | latest | MEDIUM | Programmatic Incus control, replace bash scripts with Go tests |
| **slog Testing** | `testing/slogtest` (stdlib) | Go 1.24 | HIGH | Verify slog handler implementations |

### CLI Layer

| Component | Recommendation | Version | Confidence | Rationale |
|-----------|---------------|---------|------------|-----------|
| **CLI Framework** | `github.com/spf13/cobra` | v1.10.2 | HIGH | Already used, stable, no migration needed |
| **Config** | `github.com/spf13/viper` | v1.21.0 | HIGH | Already used, stable |
| **TUI** | `github.com/charmbracelet/huh` | v0.8.0 | HIGH | Already used for interactive prompts |
| **Styling** | `github.com/charmbracelet/lipgloss` | v1.1.0 | HIGH | Already used, keep consistent |

---

## SDK Design Patterns

### Pattern 1: Functional Options for Configuration

```go
type ClientOption func(*Client)

func WithLogger(l *slog.Logger) ClientOption {
    return func(c *Client) { c.logger = l }
}

func WithProgress(r progress.Reporter) ClientOption {
    return func(c *Client) { c.reporter = r }
}

func NewClient(opts ...ClientOption) *Client {
    c := &Client{logger: slog.Default()}
    for _, opt := range opts {
        opt(c)
    }
    return c
}
```

**Confidence:** HIGH — Standard Go pattern, used in stdlib and major libraries.

### Pattern 2: Reporter as Per-Operation Parameter

```go
// Reporter passed to each operation, not stored in struct
func (c *Client) Install(ctx context.Context, cfg InstallConfig, reporter progress.Reporter) (InstallResult, error)

// Allows different reporters per operation
// Makes dependency explicit
// Easier to test
```

**Confidence:** HIGH — Explicit dependencies improve testability.

### Pattern 3: Internal Package for Non-Public Types

```
nbc/
├── client.go          # Public API: Client, InstallConfig, InstallResult
├── internal/
│   ├── installer/     # Internal implementation
│   ├── partition/     
│   ├── bootloader/    
│   └── container/     
├── cmd/               # CLI layer
└── pkg/types/         # Shared types (already clean)
```

**Confidence:** MEDIUM — May require significant restructuring. Evaluate cost/benefit.

### Pattern 4: Nil-Safe Reporter Helper

```go
type safeReporter struct {
    inner progress.Reporter
}

func safe(r progress.Reporter) *safeReporter {
    return &safeReporter{inner: r}
}

func (s *safeReporter) OnTask(t progress.ProgressTask) {
    if s.inner != nil {
        s.inner.OnTask(t)
    }
}
```

**Confidence:** HIGH — Reduces nil checks throughout code.

---

## Testing Infrastructure Patterns

### Pattern 1: Replace Sleep with Polling

```go
// BAD
time.Sleep(5 * time.Second)

// GOOD
func waitForDevice(ctx context.Context, device string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if _, err := os.Stat(device); err == nil {
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(500 * time.Millisecond):
        }
    }
    return fmt.Errorf("device %s not ready within %v", device, timeout)
}
```

**Confidence:** HIGH — Eliminates flakiness from timing assumptions.

### Pattern 2: Programmatic Incus Control

```go
import incus "github.com/lxc/incus/client"

func setupTestVM(t *testing.T) (*incus.InstanceServer, string) {
    client, err := incus.ConnectIncusUnix("", nil)
    require.NoError(t, err)
    
    vmName := fmt.Sprintf("nbc-test-%d", time.Now().UnixNano())
    // Create VM programmatically
    // ...
    
    t.Cleanup(func() {
        client.DeleteInstance(vmName)
    })
    
    return client, vmName
}
```

**Confidence:** MEDIUM — Requires learning Incus Go client, but eliminates bash script fragility.

### Pattern 3: Test Fixtures with t.TempDir()

```go
func TestInstall(t *testing.T) {
    tmpDir := t.TempDir() // Automatically cleaned up
    
    cfg := InstallConfig{
        StateDir: filepath.Join(tmpDir, "state"),
        CacheDir: filepath.Join(tmpDir, "cache"),
        // ...
    }
    // ...
}
```

**Confidence:** HIGH — Standard Go testing pattern.

---

## Logging Architecture

### Separation: Logging vs Progress

| Concern | Mechanism | Destination | Example |
|---------|-----------|-------------|---------|
| **Debug/Diagnostic** | `slog` | `/var/log/nbc/nbc.log` | "partition table read: 4 entries" |
| **User Progress** | `progress.Reporter` | stdout (via CLI reporter) | "Creating partitions (step 3/10)" |
| **Errors** | Return values | stderr (via CLI) | "failed to mount: device busy" |

### slog Configuration

```go
// SDK accepts logger, uses sensible default
func NewClient(opts ...ClientOption) *Client {
    c := &Client{
        logger: slog.New(slog.NewTextHandler(io.Discard, nil)), // silent default
    }
    for _, opt := range opts {
        opt(c)
    }
    return c
}

// CLI creates real logger
func main() {
    logFile, _ := os.OpenFile("/var/log/nbc/nbc.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))
    
    client := nbc.NewClient(nbc.WithLogger(logger))
}
```

**Confidence:** HIGH — Clean separation, standard slog patterns.

---

## What NOT to Use

| Technology | Why Avoid | Use Instead |
|------------|-----------|-------------|
| **Custom logging abstraction** | Reinventing wheel, ecosystem incompatibility | `log/slog` (stdlib) |
| **testify/suite** | Adds complexity, not needed | `testing` with helper functions |
| **mockery** | Another tool dependency | `go.uber.org/mock` (mockgen) |
| **Custom progress types in public API** | Locks consumers to internal design | `progress.Reporter` interface |
| **Global loggers/reporters** | Breaks testability, race conditions | Dependency injection |
| **time.Sleep in tests** | Non-deterministic, flaky | Polling with timeout |
| **Bash scripts for integration tests** | Fragile parsing, error handling | Go tests with Incus client |

---

## Migration Strategy

### From Current ProgressReporter to progress.Reporter

1. **Add dependency:** `go get github.com/frostyard/pm/progress@v0.2.1`
2. **Create adapter:** Bridge current `ProgressReporter` to new interface
3. **Migrate incrementally:** Update functions one-by-one to accept `progress.Reporter`
4. **Remove old:** Delete `pkg/progress.go` when fully migrated

### From Bash Tests to Go Tests

1. **Keep bash tests running** during migration (don't break CI)
2. **Add Go integration tests** alongside bash
3. **Verify parity** between bash and Go tests
4. **Remove bash tests** when Go tests have full coverage

---

## Version Verification

| Dependency | Claimed Version | Verification Date | Source |
|------------|----------------|-------------------|--------|
| `github.com/frostyard/pm/progress` | v0.2.1 | 2026-01-26 | `go list -m -versions` |
| `github.com/stretchr/testify` | v1.11.1 | 2026-01-26 | GitHub releases |
| `go.uber.org/mock` | v0.6.0 | 2026-01-26 | GitHub releases |
| `github.com/lxc/incus/client` | latest | 2026-01-26 | Incus documentation |
| `log/slog` | Go 1.21+ | 2026-01-26 | Go blog |

---

## Sources

**HIGH Confidence:**
- Go slog documentation: https://go.dev/blog/slog
- Go testing package documentation
- github.com/frostyard/pm module (verified exists)
- Existing nbc codebase analysis

**MEDIUM Confidence:**
- Incus Go client library patterns
- Community best practices for SDK design

---

*Stack research complete. Ready for roadmap creation.*
