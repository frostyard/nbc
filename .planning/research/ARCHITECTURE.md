# Architecture Patterns for Go SDK Extraction

**Research Date:** 2026-01-26
**Domain:** Go SDK extraction from CLI-entangled codebase
**Confidence:** HIGH (based on Go conventions, codebase analysis, and target interface review)

## Executive Summary

Extracting an SDK from nbc's CLI-entangled codebase requires clear component boundaries, interface-based output abstraction, and systematic refactoring. The key insight: the current `ProgressReporter` is already a step toward this goal but couples too tightly to JSON vs text modes. The target `progress.Reporter` interface from `github.com/frostyard/pm/progress` provides a cleaner abstraction with Action/Task/Step hierarchy.

The recommended approach is **inside-out refactoring**: start with the lowest-level functions, make them accept a `Reporter` interface, propagate upward, and only then restructure the public API.

---

## Component Boundaries

### Current Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLI Layer (cmd/)                          │
│  ┌─────────┬──────────┬────────────┬────────┬──────────────────┐│
│  │install  │ update   │ download   │ cache  │ interactive_     ││
│  │         │          │            │        │ install          ││
│  └────┬────┴────┬─────┴─────┬──────┴───┬────┴────┬─────────────┘│
│       │         │           │          │         │               │
│       ▼         ▼           ▼          ▼         ▼               │
│  ┌───────────────────────────────────────────────────────────────┤
│  │            Business Logic Layer (pkg/)                        │
│  ├───────────────────────────────────────────────────────────────┤
│  │  Installer      SystemUpdater      ImageCache                 │
│  │  ContainerExtractor    BootloaderInstaller                    │
│  │  PartitionScheme       LUKS operations                        │
│  │                                                               │
│  │  [ENTANGLEMENT: fmt.Print*, ProgressReporter, os.Stdout]      │
│  └───────────────────────────────────────────────────────────────┤
└─────────────────────────────────────────────────────────────────┘
```

**Current Problems:**
1. **Output entanglement**: 100+ `fmt.Print*` calls scattered across pkg/
2. **Dual-mode coupling**: `ProgressReporter` contains rendering logic (text vs JSON)
3. **Direct I/O**: `os.Stdout` references in pkg/ for JSON encoding
4. **Mixed concerns**: Confirmation prompts (`fmt.Scanln`) in business logic
5. **Callback indirection**: `InstallCallbacks` bridges pkg/ to CLI output, but lower-level functions still use `ProgressReporter`

### Target Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLI Layer (cmd/)                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  Commands: install, update, download, cache, status, etc.   ││
│  │  Responsibilities:                                           ││
│  │   - Parse flags/args                                         ││
│  │   - Create SDK client with chosen Reporter                   ││
│  │   - Handle confirmation prompts                              ││
│  │   - Render final output (success/error messages)             ││
│  └──────────────────────────┬──────────────────────────────────┘│
│                              │ Accepts: progress.Reporter        │
│                              ▼                                   │
│  ┌───────────────────────────────────────────────────────────────┤
│  │                   SDK Layer (pkg/)                            │
│  ├───────────────────────────────────────────────────────────────┤
│  │  Client (facade)                                              │
│  │   ├── Install(ctx, config, reporter) → Result, error         │
│  │   ├── Update(ctx, config, reporter) → Result, error          │
│  │   ├── Download(ctx, config, reporter) → Metadata, error      │
│  │   ├── Status(ctx) → Status, error                            │
│  │   └── Cache() → CacheManager                                  │
│  │                                                               │
│  │  Internal Components (unexported or minimal surface):         │
│  │   - installer (orchestrates installation)                     │
│  │   - updater (handles A/B updates)                             │
│  │   - extractor (OCI extraction)                                │
│  │   - bootloader (GRUB2/systemd-boot)                           │
│  │   - partition (disk partitioning)                             │
│  │   - luks (encryption)                                         │
│  │   - cache (OCI layout cache)                                  │
│  │                                                               │
│  │  [NO fmt.Print*, NO os.Stdout, NO prompts]                    │
│  │  [All output via progress.Reporter interface]                 │
│  └───────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌───────────────────────────────────────────────────────────────┤
│  │                   Types Layer (pkg/types/)                    │
│  │  - StatusOutput, InstallResult, UpdateResult                  │
│  │  - CachedImageMetadata, PartitionScheme                       │
│  │  - Error types (unchanged, already clean)                     │
│  └───────────────────────────────────────────────────────────────┤
└─────────────────────────────────────────────────────────────────┘
```

### Component Boundary Rules

| Layer | May Import | May NOT Import | Output Mechanism |
|-------|-----------|----------------|------------------|
| cmd/ | pkg/, pkg/types/ | - | fmt.Print, os.Stdout |
| pkg/ (SDK) | pkg/types/, progress.Reporter | cmd/, fmt (for output), os.Stdout | progress.Reporter only |
| pkg/types/ | standard library only | pkg/, cmd/ | None (data only) |

---

## Data Flow

### Progress/Logging Flow

**Current (entangled):**
```
install.go → ProgressReporter → {
  if json: json.Encoder → os.Stdout
  else:    fmt.Printf → os.Stdout
}

Low-level functions → fmt.Printf → os.Stdout (bypasses Reporter)
```

**Target (clean):**
```
CLI Layer:
  reporter := NewTextReporter(os.Stdout)  // or JSONReporter
  
SDK Layer:
  client.Install(ctx, config, reporter)
    └── reporter.OnAction(...)
        reporter.OnTask(...)
        reporter.OnStep(...)
        reporter.OnMessage(...)
  
  [All output flows through single interface]
```

### Progress.Reporter Interface Mapping

The target `progress.Reporter` from `github.com/frostyard/pm/progress`:

```go
type ProgressReporter interface {
    OnAction(action ProgressAction)  // High-level: "Installing system"
    OnTask(task ProgressTask)        // Mid-level: "Creating partitions"
    OnStep(step ProgressStep)        // Low-level: "Formatting /dev/sda1"
    OnMessage(msg ProgressMessage)   // Info/Warning/Error messages
}
```

**Mapping current concepts:**

| Current | Target | Notes |
|---------|--------|-------|
| `reporter.Step(n, name)` | `reporter.OnTask(...)` | Steps become Tasks |
| `reporter.Message(...)` | `reporter.OnMessage(SeverityInfo, ...)` | Explicit severity |
| `reporter.Warning(...)` | `reporter.OnMessage(SeverityWarning, ...)` | |
| `reporter.Error(...)` | `reporter.OnMessage(SeverityError, ...)` | |
| `reporter.Complete(...)` | `reporter.OnAction(...)` with EndedAt set | Action completion |
| `callbacks.OnStep` | No direct equivalent | CLI layer handles translation |

### Logging vs Progress

**Distinction:**
- **Progress** = User-facing status updates (what's happening now)
- **Logging** = Debug/diagnostic information (for troubleshooting)

**Recommendation:**
- Progress: `progress.Reporter` interface (SDK consumer controls)
- Logging: `log/slog` to `/var/log/nbc/` (always enabled, separate from progress)
- Verbose mode: Emit more progress events, not more logs

---

## Interface Design Patterns

### Pattern 1: Reporter as Dependency

**Every SDK function accepts Reporter as parameter, not stored in struct:**

```go
// Good: Reporter passed per-call
func (c *Client) Install(ctx context.Context, cfg InstallConfig, reporter progress.Reporter) (InstallResult, error)

// Bad: Reporter stored in struct (hides dependency)
type Client struct {
    reporter progress.Reporter
}
```

**Rationale:**
- Explicit dependency makes testing easier
- Different operations can use different reporters
- Consumer controls lifetime and implementation

### Pattern 2: Nil-Safe Reporter

**All internal functions handle nil reporter gracefully:**

```go
func (i *installer) createPartitions(ctx context.Context, reporter progress.Reporter) error {
    if reporter != nil {
        reporter.OnTask(progress.ProgressTask{Name: "Creating partitions", ...})
    }
    // ... actual work
}
```

Or use a helper:

```go
type safeReporter struct {
    inner progress.Reporter
}

func (s *safeReporter) OnTask(t progress.ProgressTask) {
    if s.inner != nil {
        s.inner.OnTask(t)
    }
}
```

### Pattern 3: No I/O Prompts in SDK

**Confirmation prompts belong in CLI layer:**

```go
// SDK: Returns need for confirmation, doesn't prompt
func (c *Client) PrepareInstall(ctx context.Context, cfg InstallConfig) (InstallPlan, error)

// CLI: Handles confirmation
plan, _ := client.PrepareInstall(ctx, cfg)
if !confirmDiskWipe(plan.Device) {
    return ErrCancelled
}
result, err := client.ExecuteInstall(ctx, plan, reporter)
```

Or provide confirmation callback:

```go
type InstallConfig struct {
    // ...
    ConfirmDiskWipe func(device string) bool  // nil = auto-confirm
}
```

### Pattern 4: Structured Results

**Return structured results, not formatted strings:**

```go
type InstallResult struct {
    ImageRef       string
    ImageDigest    string
    Device         string
    BootloaderType string
    Duration       time.Duration
    Warnings       []string  // Collected during install
}
```

CLI formats for display; SDK returns data.

---

## Refactoring Order

### Dependencies Between Changes

```
                    ┌──────────────────┐
                    │  Phase 5: SDK    │
                    │  Client API      │
                    └────────┬─────────┘
                             │ depends on
                             ▼
         ┌───────────────────────────────────────┐
         │  Phase 4: High-Level Orchestrators     │
         │  (Installer, SystemUpdater)            │
         └────────────────────┬──────────────────┘
                              │ depends on
                              ▼
    ┌─────────────────────────────────────────────────┐
    │  Phase 3: Mid-Level Components                   │
    │  (ContainerExtractor, BootloaderInstaller,       │
    │   ImageCache, LUKS operations)                   │
    └──────────────────────────┬──────────────────────┘
                               │ depends on
                               ▼
    ┌─────────────────────────────────────────────────┐
    │  Phase 2: Low-Level Functions                    │
    │  (partition, disk, config, dracut, fstab, etc.)  │
    └──────────────────────────┬──────────────────────┘
                               │ depends on
                               ▼
    ┌─────────────────────────────────────────────────┐
    │  Phase 1: Interfaces & Types                     │
    │  (progress.Reporter, Result types, Error types)  │
    └─────────────────────────────────────────────────┘
```

### Recommended Phase Breakdown

**Phase 1: Interface Foundation**
- Import or vendor `github.com/frostyard/pm/progress`
- Create adapter: `ProgressReporter` → `progress.Reporter`
- Define `Reporter` type alias for smooth migration
- No behavior change yet

**Phase 2: Low-Level Function Cleanup**
- Identify all `fmt.Print*` in pkg/
- Replace with `reporter.OnMessage(...)` or `reporter.OnStep(...)`
- Functions that previously returned void now accept `reporter` parameter
- Start from leaves: partition.go, disk.go, config.go
- Test each function independently

**Phase 3: Component Cleanup**
- `ContainerExtractor`: Accept reporter, remove stored Progress field
- `BootloaderInstaller`: Accept reporter, remove SetProgress method
- `ImageCache`: Accept reporter in Download/Remove/Clear methods
- LUKS functions: Accept reporter parameter

**Phase 4: Orchestrator Cleanup**
- `Installer`: Replace callbacks with single reporter
- `SystemUpdater`: Replace Progress field with reporter parameter
- Remove `callbackProgressAdapter` and similar bridges
- Move confirmation prompts to CLI layer

**Phase 5: SDK Client API**
- Create `type Client struct` as public facade
- Expose clean methods: Install, Update, Download, Status, Cache
- Hide internal types (lowercase struct names)
- Document public API with examples

**Phase 6: CLI Adaptation**
- Update cmd/ to create Reporter implementation
- Implement TextReporter (for terminal output)
- Implement JSONReporter (for --json mode)
- Handle prompts in CLI layer
- Remove deprecated ProgressReporter usages

### File-Level Refactoring Order

Based on import dependencies:

1. **pkg/types/types.go** - Already clean, no changes needed
2. **pkg/config.go** - Add reporter parameter to WriteSystemConfig, etc.
3. **pkg/disk.go** - Add reporter to WipeDisk, ValidateDisk
4. **pkg/partition.go** - Add reporter to CreatePartitions, FormatPartitions, MountPartitions
5. **pkg/luks.go** - Add reporter to SetupLUKS, OpenLUKS, EnrollTPM2
6. **pkg/dracut.go** - Add reporter to InstallDracutEtcOverlay, RegenerateInitramfs
7. **pkg/container.go** - Refactor ContainerExtractor.Extract to use reporter
8. **pkg/bootloader.go** - Refactor BootloaderInstaller.Install to use reporter
9. **pkg/cache.go** - Refactor ImageCache.Download to use reporter
10. **pkg/update.go** - Refactor SystemUpdater to use reporter
11. **pkg/install.go** - Refactor Installer to use reporter
12. **New: pkg/client.go** - Create SDK facade
13. **cmd/*.go** - Adapt CLI to new SDK

---

## Build Order Implications

### Incremental Builds Remain Working

Each phase can be merged separately:

| Phase | Can Build? | Can Test? | Behavior Change? |
|-------|------------|-----------|------------------|
| 1 | Yes | Yes | No |
| 2 | Yes | Yes | Internal only (output formatting) |
| 3 | Yes | Yes | Internal only |
| 4 | Yes | Yes | Minor (callbacks deprecated) |
| 5 | Yes | Yes | New API, old still works |
| 6 | Yes | Yes | Old API removed |

### Recommended Branch Strategy

```
main
 │
 ├── feat/sdk-progress-interface (Phase 1)
 │    └── merge when green
 │
 ├── feat/sdk-low-level-cleanup (Phase 2)
 │    └── merge when green
 │
 └── ... etc
```

Each phase is a separate PR for review isolation.

---

## Specific Entanglement Points

### Priority 1: Direct fmt.Print in pkg/

| File | Location | Current | Fix |
|------|----------|---------|-----|
| install.go:895-916 | CreateCLICallbacks | fmt.Print in callback defaults | Move to cmd/ |
| update.go:595, 747 | LUKS passphrase prompts | fmt.Scanln | Move to CLI, pass passphrase via config |
| update.go:1362-1372 | Confirmation prompt | fmt.Print + Scanln | Move to CLI |
| luks.go:179 | TPM2 enrollment message | fmt.Printf | Use reporter |
| cache.go:113, 122 | Spinner and messages | fmt.Printf | Use reporter |

### Priority 2: ProgressReporter os.Stdout Coupling

| File | Issue | Fix |
|------|-------|-----|
| progress.go:40 | `json.NewEncoder(os.Stdout)` | Remove; CLI provides Reporter |
| progress.go | Text vs JSON logic | Remove; CLI chooses Reporter impl |

### Priority 3: Callbacks Indirection

| File | Issue | Fix |
|------|-------|-----|
| install.go | InstallCallbacks + callbackProgressAdapter | Replace with single Reporter |
| install.go | CreateCLICallbacks helper | Move to cmd/ |

---

## Quality Checklist

- [x] Components clearly defined with boundaries
- [x] Data flow direction explicit
- [x] Build order implications noted
- [x] Interface design patterns documented
- [x] Specific entanglement points cataloged
- [x] Phase dependencies mapped

---

## Sources

| Source | Confidence | Content |
|--------|------------|---------|
| Codebase analysis | HIGH | Current architecture, entanglement patterns |
| github.com/frostyard/pm/progress | HIGH | Target progress.Reporter interface |
| Go standard library patterns | HIGH | Interface design, package structure |
| .planning/PROJECT.md | HIGH | Requirements and constraints |

---

*Architecture research complete. Ready for roadmap creation.*
