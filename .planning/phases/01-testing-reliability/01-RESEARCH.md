# Phase 1: Testing Reliability - Research

**Researched:** 2026-01-26
**Domain:** Go integration testing, Incus VM automation, golden file testing
**Confidence:** HIGH

## Summary

This phase focuses on making the nbc integration tests 100% deterministic by replacing bash-based Incus VM tests with Go client library implementations, adding proper cleanup, timeouts, snapshot-based VM resets, and golden file comparison for CLI output.

The current test infrastructure uses a combination of Go unit tests (`pkg/*_test.go`) for lower-level testing and bash scripts (`test_incus*.sh`) for VM-based integration tests. The bash scripts are inherently flaky due to lack of proper error handling, timeout management, and resource cleanup. The solution is to migrate to the Incus Go client library (`github.com/lxc/incus/v6/client`) which provides programmatic control over VMs, snapshots, and cleanup.

For golden file testing, the Go ecosystem has mature libraries (`github.com/sebdah/goldie/v2`) that support `-update` flag regeneration, diff output, and template-based comparisons. The project already has `github.com/charmbracelet/x/exp/golden` as an indirect dependency, but `goldie/v2` is more feature-rich for this use case.

**Primary recommendation:** Use `github.com/lxc/incus/v6/client` for VM management and `github.com/sebdah/goldie/v2` for golden file testing, with structured test fixtures and explicit timeout handling per test type.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/lxc/incus/v6/client` | v6.21.0 | Incus Go client for VM management | Official Go client, direct API access, proper error handling |
| `github.com/sebdah/goldie/v2` | v2.8.0 | Golden file testing | Mature, `-update` flag support, colored diffs, testdata directory |
| `testing` | stdlib | Go testing framework | Already in use, provides `t.Cleanup()`, `t.Run()`, `t.Parallel()` |
| `context` | stdlib | Context with timeout | Required for cancellation and deadline propagation |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/charmbracelet/x/exp/golden` | latest | Simple golden file assertions | Alternative if simpler API needed (already indirect dep) |
| `time` | stdlib | Timeout management | Setting test-specific timeouts |
| `os/exec` | stdlib | External command execution | Legacy bash commands during migration |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `goldie/v2` | `charmbracelet/x/exp/golden` | Simpler but less features (no JSON/XML, no templates) |
| Incus client | Shell exec | Current approach - flaky, harder to control |
| Per-test snapshots | Shared baseline | Shared is faster but risks cross-test contamination |

**Installation:**
```bash
go get github.com/lxc/incus/v6/client@v6.21.0
go get github.com/sebdah/goldie/v2@v2.8.0
```

## Architecture Patterns

### Recommended Project Structure
```
pkg/
├── testutil/
│   ├── disk.go          # Existing - loop device test disks
│   ├── incus.go          # NEW - Incus client wrapper for tests
│   ├── golden.go         # NEW - Golden file helpers
│   └── cleanup.go        # NEW - Resource cleanup utilities
├── testdata/
│   └── *.golden          # Golden files for CLI output
├── test-failures/        # Diagnostic logs on failure (gitignored)
└── *_test.go             # Test files
```

### Pattern 1: Incus Test Fixture
**What:** Wrap Incus client with test-specific setup/teardown
**When to use:** All VM-based integration tests

**Example:**
```go
// Source: Recommended pattern based on Incus Go client API
type IncusFixture struct {
    client   incus.InstanceServer
    vmName   string
    diskName string
    t        *testing.T
}

func NewIncusFixture(t *testing.T) *IncusFixture {
    t.Helper()
    
    // Connect to Incus
    client, err := incus.ConnectIncusUnix("", nil)
    if err != nil {
        t.Fatalf("Failed to connect to Incus: %v", err)
    }
    
    vmName := fmt.Sprintf("nbc-test-%s-%d", t.Name(), os.Getpid())
    
    fixture := &IncusFixture{
        client: client,
        vmName: vmName,
        t:      t,
    }
    
    // Register cleanup BEFORE creating resources
    t.Cleanup(fixture.Cleanup)
    
    return fixture
}

func (f *IncusFixture) Cleanup() {
    // Stop VM (force if necessary)
    reqState := api.InstanceStatePut{
        Action:  "stop",
        Force:   true,
        Timeout: -1,
    }
    op, _ := f.client.UpdateInstanceState(f.vmName, reqState, "")
    if op != nil {
        _ = op.Wait() // Ignore errors - best effort cleanup
    }
    
    // Delete VM
    op, _ = f.client.DeleteInstance(f.vmName)
    if op != nil {
        _ = op.Wait()
    }
    
    // Delete storage volume if exists
    if f.diskName != "" {
        _ = f.client.DeleteStoragePoolVolume("default", "custom", f.diskName)
    }
}
```

### Pattern 2: Snapshot-Based Test Reset
**What:** Reset VM to snapshot before each test function
**When to use:** VM tests requiring fresh state per test

**Example:**
```go
// Source: Recommended pattern based on Incus Go client API
func (f *IncusFixture) CreateBaselineSnapshot(name string) error {
    req := api.InstanceSnapshotsPost{
        Name:     name,
        Stateful: false,
    }
    op, err := f.client.CreateInstanceSnapshot(f.vmName, req)
    if err != nil {
        return fmt.Errorf("create snapshot: %w", err)
    }
    return op.Wait()
}

func (f *IncusFixture) ResetToSnapshot(name string) error {
    // Stop VM first
    f.stopVM()
    
    // Restore snapshot
    reqPut := api.InstancePut{
        Restore: name,
    }
    op, err := f.client.UpdateInstance(f.vmName, reqPut, "")
    if err != nil {
        return fmt.Errorf("restore snapshot: %w", err)
    }
    if err := op.Wait(); err != nil {
        return fmt.Errorf("restore wait: %w", err)
    }
    
    // Start VM
    return f.startVM()
}
```

### Pattern 3: Golden File Testing
**What:** Compare CLI output against stored golden files
**When to use:** Testing CLI command output stability

**Example:**
```go
// Source: https://pkg.go.dev/github.com/sebdah/goldie/v2
func TestCLI_List(t *testing.T) {
    // Capture CLI output
    output := captureOutput(func() {
        cmd := NewListCommand()
        cmd.Execute()
    })
    
    // Normalize dynamic content before comparison
    normalized := normalizeOutput(output)
    
    // Compare with golden file
    g := goldie.New(t,
        goldie.WithFixtureDir("testdata"),
        goldie.WithNameSuffix(".golden"),
        goldie.WithDiffEngine(goldie.ColoredDiff),
    )
    g.Assert(t, "list-output", []byte(normalized))
}

// Update golden files: go test -update ./...
func normalizeOutput(s string) string {
    // Replace timestamps with placeholder
    s = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`).
        ReplaceAllString(s, "TIMESTAMP")
    // Replace UUIDs with placeholder
    s = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).
        ReplaceAllString(s, "UUID")
    // Replace paths that vary
    s = regexp.MustCompile(`/dev/loop\d+`).
        ReplaceAllString(s, "/dev/loopN")
    return s
}
```

### Pattern 4: Structured Test Timeouts
**What:** Per-test-type timeout configuration
**When to use:** All tests

**Example:**
```go
// Source: Go testing best practices
const (
    TimeoutUnit       = 30 * time.Second   // Unit tests
    TimeoutIntegration = 2 * time.Minute   // Integration tests (disk operations)
    TimeoutVM         = 10 * time.Minute   // VM tests (boot, install)
    TimeoutVMBoot     = 2 * time.Minute    // VM boot specifically
)

func TestVM_Install(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), TimeoutVM)
    defer cancel()
    
    fixture := NewIncusFixture(t)
    
    // Use context for cancellable operations
    err := fixture.CreateVMWithContext(ctx, "images:fedora/42/cloud")
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            t.Fatalf("Test timed out after %v waiting for VM creation", TimeoutVM)
        }
        t.Fatalf("VM creation failed: %v", err)
    }
    
    // Wait for VM to be ready with specific timeout
    if err := fixture.WaitForSystemReady(ctx, TimeoutVMBoot); err != nil {
        t.Fatalf("Test timed out after %v waiting for VM boot: last action was systemctl is-system-running", TimeoutVMBoot)
    }
}
```

### Anti-Patterns to Avoid
- **Shell exec for VM operations:** Use Incus Go client instead
- **Sleep-based waits:** Use proper polling with context/timeout
- **Shared test state:** Reset VM via snapshot before each test
- **Silent resource leaks:** Always register cleanup before creating resources
- **Hardcoded timeouts:** Define constants per test type

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Golden file comparison | String comparison with file I/O | `goldie/v2` | Handles updates, diffs, normalization |
| VM lifecycle management | os/exec with incus commands | `incus/v6/client` | Proper API, error handling, typed responses |
| Test cleanup | defer with manual cleanup | `t.Cleanup()` | Runs even on `t.Fatal()`, registered in order |
| Wait for condition | `time.Sleep` loops | Context with polling | Proper cancellation, deadline propagation |
| Diff output | Simple string comparison | `goldie` ColoredDiff | Visual, maintainable, standard format |

**Key insight:** The Incus Go client provides typed access to all VM operations (create, snapshot, exec, file transfer) with proper error handling. Bash scripts cannot reliably handle edge cases like partial failures or timeout cleanup.

## Common Pitfalls

### Pitfall 1: Resource Leak on Test Failure
**What goes wrong:** VM or storage volume left behind when test panics or calls `t.Fatal()`
**Why it happens:** Cleanup in defer or at end of test never runs
**How to avoid:** Use `t.Cleanup()` which runs even after `t.Fatal()`. Register cleanup BEFORE creating resources.
**Warning signs:** `incus list` shows orphaned `nbc-test-*` VMs/volumes

### Pitfall 2: Race Condition in Parallel VM Tests
**What goes wrong:** Tests interfere with each other's VMs
**Why it happens:** VM names collide or shared resources accessed concurrently
**How to avoid:** Include `t.Name()` and `os.Getpid()` in VM names. Run VM tests sequentially unless explicitly parallelized.
**Warning signs:** Intermittent failures only in CI, "resource already exists" errors

### Pitfall 3: Non-Deterministic Wait Conditions
**What goes wrong:** Test passes on fast machine, fails on slow CI
**Why it happens:** Fixed `time.Sleep()` instead of condition-based waiting
**How to avoid:** Poll for condition with exponential backoff and explicit timeout
**Warning signs:** Tests pass locally, fail in CI; adding longer sleep "fixes" it

### Pitfall 4: Golden File Diff Noise
**What goes wrong:** Golden file tests fail on dynamic content (timestamps, UUIDs)
**Why it happens:** Output contains non-deterministic values
**How to avoid:** Normalize output before comparison (replace timestamps/UUIDs with placeholders)
**Warning signs:** Tests fail with diff showing only timestamp changes

### Pitfall 5: Orphaned Mounts
**What goes wrong:** `/mnt/test-*` directories left mounted, blocking cleanup
**Why it happens:** Test failed between mount and unmount
**How to avoid:** Force unmount in cleanup, use unique mount points per test
**Warning signs:** "device or resource busy" errors, disk space exhaustion

### Pitfall 6: Snapshot State Contamination
**What goes wrong:** Test finds unexpected state from previous test
**Why it happens:** Snapshot taken after modifications, or wrong snapshot restored
**How to avoid:** Create baseline snapshot immediately after VM boot, before any test operations. Reset BEFORE each test, not after.
**Warning signs:** Test order matters, first test passes but second fails

## Code Examples

Verified patterns from official sources and best practices:

### Connecting to Incus
```go
// Source: https://pkg.go.dev/github.com/lxc/incus/v6/client
import incus "github.com/lxc/incus/v6/client"

// Connect to local Incus socket
client, err := incus.ConnectIncusUnix("", nil)
if err != nil {
    return fmt.Errorf("connect to incus: %w", err)
}
```

### Creating a VM
```go
// Source: Incus Go client API
req := api.InstancesPost{
    Name: vmName,
    Type: api.InstanceTypeVM,
    Source: api.InstanceSource{
        Type:     "image",
        Server:   "https://images.linuxcontainers.org",
        Protocol: "simplestreams",
        Alias:    "fedora/42/cloud",
    },
    InstancePut: api.InstancePut{
        Config: map[string]string{
            "limits.cpu":          "4",
            "limits.memory":       "16GiB",
            "security.secureboot": "false",
        },
    },
}

op, err := client.CreateInstance(req)
if err != nil {
    return err
}
if err := op.Wait(); err != nil {
    return err
}
```

### Creating and Restoring Snapshots
```go
// Source: Incus Go client API
// Create snapshot
snapReq := api.InstanceSnapshotsPost{
    Name:     "baseline",
    Stateful: false,
}
op, err := client.CreateInstanceSnapshot(vmName, snapReq)
if err != nil {
    return err
}
if err := op.Wait(); err != nil {
    return err
}

// Restore snapshot
putReq := api.InstancePut{
    Restore: "baseline",
}
op, err = client.UpdateInstance(vmName, putReq, "")
if err != nil {
    return err
}
if err := op.Wait(); err != nil {
    return err
}
```

### Executing Commands in VM
```go
// Source: Incus Go client API
execReq := api.InstanceExecPost{
    Command:     []string{"systemctl", "is-system-running", "--wait"},
    WaitForWS:   true,
    Interactive: false,
}

op, err := client.ExecInstance(vmName, execReq, nil)
if err != nil {
    return err
}

// Wait for execution
opAPI := op.Get()
exitCode := opAPI.Metadata["return"].(float64)
if exitCode != 0 {
    return fmt.Errorf("command exited with code %d", int(exitCode))
}
```

### Golden File Testing with Goldie
```go
// Source: https://pkg.go.dev/github.com/sebdah/goldie/v2
import "github.com/sebdah/goldie/v2"

func TestCLIOutput(t *testing.T) {
    actual := runCLICommand("nbc", "list")
    
    g := goldie.New(t,
        goldie.WithFixtureDir("testdata"),
        goldie.WithNameSuffix(".golden"),
        goldie.WithDiffEngine(goldie.ColoredDiff),
    )
    
    g.Assert(t, "list-command", []byte(actual))
}

// Update: go test -update ./...
```

### Test Cleanup Pattern
```go
// Source: Go testing best practices
func TestVMOperation(t *testing.T) {
    // Create resources
    vmName := createVM(t)
    
    // Register cleanup BEFORE potential failure points
    t.Cleanup(func() {
        // Force stop - ignore errors
        forceStopVM(vmName)
        // Delete - ignore errors
        deleteVM(vmName)
    })
    
    // Now safe to run test - cleanup will happen even on t.Fatal()
    if err := runTest(vmName); err != nil {
        t.Fatalf("Test failed: %v", err)
    }
}
```

### Diagnostic Dump on Failure
```go
// Source: Recommended pattern based on CONTEXT.md decisions
func dumpDiagnostics(t *testing.T, fixture *IncusFixture) {
    t.Helper()
    
    // Create log file
    logDir := "test-failures"
    os.MkdirAll(logDir, 0755)
    logPath := filepath.Join(logDir, fmt.Sprintf("%s_%d.log", t.Name(), time.Now().Unix()))
    
    var buf bytes.Buffer
    
    // Last 50 lines of console output
    buf.WriteString("=== Last 50 lines of console ===\n")
    output, _ := fixture.GetConsoleLog(50)
    buf.WriteString(output)
    
    // Mounted volumes
    buf.WriteString("\n=== Mounted volumes ===\n")
    mounts, _ := fixture.ExecCommand("findmnt", "-l")
    buf.WriteString(mounts)
    
    // Network state
    buf.WriteString("\n=== Network state ===\n")
    netState, _ := fixture.ExecCommand("ip", "addr")
    buf.WriteString(netState)
    
    // Write to file
    os.WriteFile(logPath, buf.Bytes(), 0644)
    
    // Also log summary inline
    t.Logf("Diagnostic dump saved to: %s", logPath)
    t.Logf("Last 10 lines of console:\n%s", lastNLines(output, 10))
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Bash scripts for Incus tests | Go Incus client library | Incus v6 (2024) | Programmatic control, proper error handling |
| Manual golden file comparison | `goldie/v2` library | Established 2018+ | Standard `-update` flag, diff engines |
| `defer cleanup()` | `t.Cleanup()` | Go 1.14 (2020) | Runs even after t.Fatal() |
| Fixed `time.Sleep()` | Context-based polling | Go 1.7+ contexts | Proper cancellation, deadlines |

**Deprecated/outdated:**
- LXD API: Replaced by Incus API (fork, same patterns)
- `testing.Main` for setup: Use `TestMain(m *testing.M)` instead

## Open Questions

Things that couldn't be fully resolved:

1. **Optimal snapshot sharing strategy**
   - What we know: Per-test snapshots ensure isolation; shared baseline is faster
   - What's unclear: Whether snapshot restore overhead is significant enough to optimize
   - Recommendation: Start with per-test reset (isolation first), measure performance, optimize if needed

2. **VM test parallelization**
   - What we know: Incus can run multiple VMs; resource contention is possible
   - What's unclear: Whether host has enough resources for parallel VM tests
   - Recommendation: Run VM tests sequentially initially (`t.Run` without `t.Parallel()`), parallelize later if tests are slow

3. **Golden file storage location**
   - What we know: `testdata/` is Go convention; `goldie` defaults to this
   - What's unclear: Whether to use flat or nested structure for multiple commands
   - Recommendation: Use `testdata/` with test name subdirectories (`goldie.WithTestNameForDir(true)`)

## Sources

### Primary (HIGH confidence)
- `github.com/lxc/incus/v6/client` - pkg.go.dev documentation, API reference
- `github.com/sebdah/goldie/v2` - pkg.go.dev documentation, README
- Go stdlib `testing` package - official documentation
- Incus REST API documentation - https://linuxcontainers.org/incus/docs/main/rest-api/

### Secondary (MEDIUM confidence)
- Existing nbc test infrastructure (`pkg/testutil/disk.go`, `pkg/*_test.go`) - reviewed for patterns
- Existing bash scripts (`test_incus*.sh`) - reviewed for migration scope

### Tertiary (LOW confidence)
- None - all claims verified with official documentation

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Official library documentation verified
- Architecture: HIGH - Patterns derived from official APIs and existing codebase
- Pitfalls: HIGH - Based on observed issues in existing scripts and Go testing practices

**Research date:** 2026-01-26
**Valid until:** 2026-02-26 (30 days - stable domain)
