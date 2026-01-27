# Coding Conventions

**Analysis Date:** 2026-01-26

## Naming Patterns

**Files:**
- Lowercase with underscores for multi-word names: `device_detect.go`, `etc_persistence.go`
- Test files: `*_test.go` suffix (e.g., `config_test.go`, `disk_test.go`)
- No prefix/suffix patterns for type files; named by primary concern

**Functions:**
- Exported: PascalCase (`CreatePartitions`, `NewInstaller`, `GetRemoteImageDigest`)
- Unexported: camelCase (`getDiskInfo`, `findPartitionByUUID`, `buildKernelCmdline`)
- Constructor pattern: `New<Type>` (e.g., `NewLinter`, `NewProgressReporter`, `NewInstaller`)
- Boolean getters: `Is<Thing>` (e.g., `IsNBCBooted`, `IsBlockDevice`, `IsRunningInContainer`)

**Variables:**
- Local: camelCase (`mountPoint`, `deviceName`, `imageRef`)
- Package-level constants: PascalCase for exported, camelCase for unexported
- Struct fields: PascalCase (exported), camelCase (unexported)

**Types:**
- Structs: PascalCase (`DiskInfo`, `PartitionScheme`, `InstallConfig`)
- Interfaces: PascalCase, typically with `-er` suffix where applicable
- Type aliases: Used for backward compatibility (`type LintSeverity = types.LintSeverity`)
- Constants: PascalCase, grouped with `const` blocks

**Packages:**
- Single word, lowercase: `pkg`, `cmd`, `types`, `testutil`

## Code Style

**Formatting:**
- `go fmt` - standard Go formatting
- Run via: `make fmt`

**Linting:**
- `golangci-lint` (optional, run via `make lint`)
- Errors skipped if golangci-lint not installed

**Line Length:**
- No strict enforcement; idiomatic Go line lengths

**Braces:**
- Opening brace on same line (Go standard)
- Always use braces for control structures

## Import Organization

**Order:**
1. Standard library imports
2. Third-party imports
3. Internal project imports (`github.com/frostyard/nbc/...`)

**Examples from codebase:**
```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/testutil"
)
```

**Path Aliases:**
- None used; full import paths throughout

## Error Handling

**Patterns:**
- Return errors as second return value: `func Foo() (result, error)`
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Use `errors.New()` for static error messages
- Check errors immediately after function calls

**Error wrapping examples:**
```go
if err := os.MkdirAll(configDir, 0755); err != nil {
    return fmt.Errorf("failed to create config directory: %w", err)
}

data, err := os.ReadFile(configPath)
if err != nil {
    return nil, fmt.Errorf("failed to read config file: %w", err)
}
```

**Context cancellation:**
```go
if err := ctx.Err(); err != nil {
    return result, err
}
```

**Nil-safe callback invocation:**
```go
func (i *Installer) callOnMessage(message string) {
    if i.callbacks != nil && i.callbacks.OnMessage != nil {
        i.callbacks.OnMessage(message)
    }
}
```

## Logging

**Framework:** Direct `fmt` output via `ProgressReporter`

**Patterns:**
- Use `ProgressReporter` for all user-facing output
- `Message()` for standard info (indented in text mode)
- `MessagePlain()` for non-indented output
- `Warning()` for warnings (prefixed with "Warning:")
- `Error()` for errors
- Supports both text and JSON Lines output modes

**Example:**
```go
progress.Message("Installing bootloader to %s", device)
progress.Warning("initramfs regeneration failed: %v", err)
```

## Comments

**When to Comment:**
- Package-level documentation with usage examples
- Exported functions with purpose and parameter descriptions
- Complex logic that isn't self-explanatory
- Constants that have special meaning

**Package Documentation:**
```go
// Package pkg provides the public API for nbc (bootc container installation).
//
// The primary entry point is the Installer type, which handles installation
// of bootc container images to physical disks or loopback devices.
//
// Example usage:
//
//	cfg := &pkg.InstallConfig{...}
```

**Function Documentation:**
```go
// GetRemoteImageDigest fetches the digest of a remote container image without downloading layers.
// Returns the digest in the format "sha256:..."
func GetRemoteImageDigest(imageRef string) (string, error)
```

**Inline Comments:**
- Used sparingly for non-obvious logic
- Explain "why" not "what"

## Function Design

**Size:** 
- Functions generally under 100 lines
- Complex operations broken into helper functions
- Large install flow uses step-based structure with callbacks

**Parameters:**
- Context first when present: `func Foo(ctx context.Context, ...)`
- Required params before optional
- Use structs for many configuration options (e.g., `InstallConfig`)
- DryRun and ProgressReporter commonly passed together

**Return Values:**
- `(value, error)` pattern is standard
- Use named returns sparingly, mainly in complex functions
- Always check errors before using return values

**Example patterns:**
```go
// Single return with error
func ReadSystemConfig() (*SystemConfig, error)

// No return, just error
func WipeDisk(ctx context.Context, device string, dryRun bool, progress *ProgressReporter) error

// Multiple returns
func GetInactiveRootPartition(scheme *PartitionScheme) (string, bool, error)
```

## Module Design

**Exports:**
- Exported types and functions use PascalCase
- Keep internal helpers unexported
- Re-export types from sub-packages for convenience:
```go
// Type aliases for backward compatibility
type LintSeverity = types.LintSeverity
type LintIssue = types.LintIssue

// Re-export constants for backward compatibility
const (
    SeverityError   = types.SeverityError
    SeverityWarning = types.SeverityWarning
)
```

**Barrel Files:**
- Not used; Go doesn't have this pattern
- Each file contains related functionality

**Package Structure:**
- `pkg/` contains core library code
- `pkg/types/` contains JSON-serializable output types
- `pkg/testutil/` contains test helpers
- `cmd/` contains CLI commands (cobra)

## Struct Patterns

**Configuration Structs:**
```go
type InstallConfig struct {
    ImageRef       string              // Required field
    Device         string              // Required unless Loopback set
    FilesystemType string              // Optional with default
    Encryption     *EncryptionOptions  // Optional pointer (nil = disabled)
    Loopback       *LoopbackOptions    // Optional pointer
    Verbose        bool                // Flag
    DryRun         bool                // Flag
}
```

**Validation Method:**
```go
func (c *InstallConfig) Validate() error {
    if c.ImageRef == "" && c.LocalImage == nil {
        return errors.New("either ImageRef or LocalImage is required")
    }
    // ...
    return nil
}
```

**Builder/Factory Pattern:**
```go
func NewInstaller(cfg *InstallConfig) (*Installer, error) {
    // Apply defaults
    if cfg.FilesystemType == "" {
        cfg.FilesystemType = "btrfs"
    }
    // Validate
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }
    return &Installer{config: cfg}, nil
}
```

## Callback Pattern

**For progress reporting and extensibility:**
```go
type InstallCallbacks struct {
    OnStep     func(step, totalSteps int, name string)
    OnProgress func(percent int, message string)
    OnMessage  func(message string)
    OnWarning  func(message string)
    OnError    func(err error, message string)
}
```

**Usage:**
```go
installer.SetCallbacks(&pkg.InstallCallbacks{
    OnStep: func(step, total int, name string) {
        fmt.Printf("[%d/%d] %s\n", step, total, name)
    },
})
```

## JSON Output Support

**Pattern:** Dual-mode output (text and JSON)

**Implementation:**
```go
type ProgressReporter struct {
    enabled    bool  // JSON mode enabled
    encoder    *json.Encoder
}

func (p *ProgressReporter) Message(format string, args ...any) {
    msg := fmt.Sprintf(format, args...)
    if p.enabled {
        p.emit(ProgressEvent{Type: EventTypeMessage, Message: msg})
    } else {
        fmt.Printf("  %s\n", msg)
    }
}
```

**Output types in `pkg/types/`:**
```go
type StatusOutput struct {
    Image          string       `json:"image"`
    Digest         string       `json:"digest,omitempty"`
    Device         string       `json:"device"`
    // ...
}
```

## Dry-Run Pattern

**Consistent across all write operations:**
```go
func WipeDisk(ctx context.Context, device string, dryRun bool, progress *ProgressReporter) error {
    if dryRun {
        if progress != nil {
            progress.Message("[DRY RUN] Would wipe disk: %s", device)
        }
        return nil
    }
    // Actual implementation...
}
```

## File Permissions

**Standard permissions used:**
- Directories: `0755` (rwxr-xr-x)
- Config files: `0644` (rw-r--r--)
- Private keys/passwords: `0600` (rw-------)

---

*Convention analysis: 2026-01-26*
