# clix Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hand-rolled CLI infrastructure with `github.com/frostyard/clix` for standardized version injection, flags, JSON output, and reporter factory.

**Architecture:** `main.go` creates `clix.App` and calls `Run(RootCmd)`. All `cmd/` files use `clix.Verbose`, `clix.DryRun`, `clix.JSONOutput`, `clix.Silent` instead of viper lookups. Reporter creation uses `clix.NewReporter()`. JSON output uses `clix.OutputJSON()` and `clix.OutputJSONError()`.

**Tech Stack:** Go 1.26, github.com/frostyard/clix, cobra, fang

**Design doc:** `docs/plans/2026-03-04-clix-migration-design.md`

---

### Task 1: Add clix dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run: `go get github.com/frostyard/clix@latest`

**Step 2: Verify it resolved**

Run: `grep clix go.mod`
Expected: `github.com/frostyard/clix v0.X.X`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/frostyard/clix"
```

---

### Task 2: Rewrite main.go and root.go (foundation)

**Files:**
- Modify: `main.go`
- Modify: `cmd/root.go`

**Step 1: Rewrite main.go**

Replace the entire file with:

```go
package main

import (
	"fmt"
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/cmd"
)

// version is set by ldflags during build
var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}
	if err := app.Run(cmd.RootCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 2: Rewrite root.go**

Replace the entire file with:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

// RootCmd is the root command for nbc. Exported for main.go to pass to clix.App.Run().
var RootCmd = &cobra.Command{
	Use:   "nbc",
	Short: "A bootc container installer for physical disks",
	Long: `nbc is a tool for installing bootc compatible containers to physical disks.
It automates the process of preparing disks and deploying bootable container images.`,
}
```

This removes: `SetVersion`, `SetCommit`, `SetDate`, `SetBuiltBy`, `makeVersionString`, `Execute`, `init()` with flag registration and viper binding. All imports of `fang`, `viper`, `context`, `fmt`, `os` are removed.

**Step 3: Update all `rootCmd` references to `RootCmd`**

Every `cmd/*.go` file that references `rootCmd` in its `init()` function needs updating. The pattern is `rootCmd.AddCommand(...)` → `RootCmd.AddCommand(...)`.

Files to update (search for `rootCmd`):
- `cmd/install.go`: `rootCmd.AddCommand(installCmd)` → `RootCmd.AddCommand(installCmd)`
- `cmd/update.go`: `rootCmd.AddCommand(updateCmd)` → `RootCmd.AddCommand(updateCmd)`
- `cmd/list.go`: `rootCmd.AddCommand(listCmd)` → `RootCmd.AddCommand(listCmd)`
- `cmd/status.go`: `rootCmd.AddCommand(statusCmd)` → `RootCmd.AddCommand(statusCmd)`
- `cmd/cache.go`: `rootCmd.AddCommand(cacheCmd)` → `RootCmd.AddCommand(cacheCmd)`
- `cmd/download.go`: `rootCmd.AddCommand(downloadCmd)` → `RootCmd.AddCommand(downloadCmd)`
- `cmd/validate.go`: `rootCmd.AddCommand(validateCmd)` → `RootCmd.AddCommand(validateCmd)`
- `cmd/lint.go`: `rootCmd.AddCommand(lintCmd)` → `RootCmd.AddCommand(lintCmd)`
- `cmd/interactive_install.go`: `rootCmd.AddCommand(interactiveInstallCmd)` → `RootCmd.AddCommand(interactiveInstallCmd)`
- `cmd/gendocs.go`: check for `rootCmd` reference → `RootCmd`

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: Success (may have unused import warnings until other files are migrated)

Note: This step may fail if other files still import viper and reference flags that no longer exist on rootCmd. That's expected — subsequent tasks will fix those files. If the build fails, continue to Task 3 immediately.

**Step 5: Commit**

```bash
git add main.go cmd/root.go cmd/*.go
git commit -m "refactor: replace CLI infrastructure with clix (foundation)"
```

---

### Task 3: Migrate list.go — remove shared JSON helpers

**Files:**
- Modify: `cmd/list.go`

**Step 1: Rewrite list.go**

Key changes:
1. Replace `viper` import with `clix` import
2. Remove `encoding/json` and `os` imports (no longer needed)
3. Replace `viper.GetBool("verbose")` → `clix.Verbose`
4. Replace `viper.GetBool("json")` → `clix.JSONOutput`
5. Replace `return outputJSON(output)` → `clix.OutputJSON(output); return nil`
6. Replace `return outputJSONError(...)` → `return clix.OutputJSONError(...)`
7. **Delete** the `outputJSON()` and `outputJSONError()` functions (lines 102-118)

The `outputJSON` call sites change because `clix.OutputJSON` returns `bool` not `error`:
- Old: `return outputJSON(output)` → New: `clix.OutputJSON(output); return nil`

Updated imports:

```go
import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)
```

Updated `runList` function (replace viper calls and outputJSON calls):

```go
func runList(cmd *cobra.Command, args []string) error {
	verbose := clix.Verbose
	jsonOutput := clix.JSONOutput

	disks, err := pkg.ListDisks()
	if err != nil {
		if jsonOutput {
			return clix.OutputJSONError("failed to list disks", err)
		}
		return fmt.Errorf("failed to list disks: %w", err)
	}

	if jsonOutput {
		// ... build output struct unchanged ...
		clix.OutputJSON(output)
		return nil
	}
	// ... rest of text output unchanged ...
}
```

Delete the `outputJSON` and `outputJSONError` functions entirely.

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`
Expected: May fail — other files still call `outputJSON`/`outputJSONError`. Continue to next tasks.

**Step 3: Commit (or defer commit until all files compile)**

---

### Task 4: Migrate status.go and validate.go

**Files:**
- Modify: `cmd/status.go`
- Modify: `cmd/validate.go`

These files use `viper.GetBool` and `outputJSON`/`outputJSONError` but do NOT create reporters.

**Step 1: Migrate status.go**

Replace imports: remove `viper`, add `clix`.

```go
import (
	"fmt"
	"strings"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)
```

In `runStatus`:
- `viper.GetBool("verbose")` → `clix.Verbose`
- `viper.GetBool("json")` → `clix.JSONOutput`
- `outputJSONError(...)` → `clix.OutputJSONError(...)`
- `return outputJSON(output)` → `clix.OutputJSON(output); return nil`

**Step 2: Migrate validate.go**

Replace imports: remove `viper`, add `clix`.

```go
import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)
```

In `runValidate`:
- `viper.GetBool("verbose")` → `clix.Verbose`
- `viper.GetBool("json")` → `clix.JSONOutput`
- `return outputJSON(output)` → `clix.OutputJSON(output); return nil`

---

### Task 5: Migrate install.go

**Files:**
- Modify: `cmd/install.go`

**Step 1: Update imports**

Replace `viper` with `clix`. Keep `reporter` import (still needed for `reporter.Reporter` type in `buildInstallConfig`).

```go
import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)
```

**Step 2: Update runInstall**

```go
func runInstall(cmd *cobra.Command, args []string) error {
	cfg, err := buildInstallConfig(cmd.Context())
	if err != nil {
		return err
	}
	// ... rest unchanged (installer creation, Install call, loopback instructions) ...
	// Replace jsonOutput check for loopback with clix.JSONOutput:
	if result.LoopbackPath != "" && !clix.JSONOutput {
		// ... print instructions unchanged ...
	}
	return nil
}
```

**Step 3: Update buildInstallConfig**

Remove verbose, dryRun, jsonOutput parameters — use clix vars directly:

```go
func buildInstallConfig(ctx context.Context) (*pkg.InstallConfig, error) {
	progress := clix.NewReporter()
	reportError := func(err error, msg string) error {
		progress.Error(err, msg)
		return err
	}
	// ... rest of function unchanged, but replace parameter references:
	// verbose → clix.Verbose
	// dryRun → clix.DryRun
	// jsonOutput → clix.JSONOutput
	// ...
}
```

Everywhere in buildInstallConfig that references `verbose`, `dryRun`, or `jsonOutput` as local variables, replace with `clix.Verbose`, `clix.DryRun`, `clix.JSONOutput`.

---

### Task 6: Migrate update.go

**Files:**
- Modify: `cmd/update.go`

This is the most complex file because of the `--check --json` NoopReporter case and the `--force` viper binding.

**Step 1: Update imports**

Replace `viper` with `clix`. Keep `reporter` (needed for `reporter.NoopReporter{}`). Keep `encoding/json` (used for direct JSON encoding in update-specific output).

```go
import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)
```

**Step 2: Remove viper.BindPFlag for "force" in init()**

The `--force` flag is already registered on `updateCmd.Flags()`. Replace viper binding with direct flag access in `runUpdate`. Delete line 82: `_ = viper.BindPFlag("force", updateCmd.Flags().Lookup("force"))`.

**Step 3: Update runUpdate**

```go
func runUpdate(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		force = true
	}

	// Special reporter logic for --check --json: suppress streaming progress
	var progress reporter.Reporter
	if clix.JSONOutput && updFlags.checkOnly {
		progress = reporter.NoopReporter{}
	} else {
		progress = clix.NewReporter()
	}

	// ... rest of function: replace all remaining references:
	// verbose → clix.Verbose
	// dryRun → clix.DryRun
	// jsonOutput → clix.JSONOutput
	// force stays as local var (from cmd.Flags())
}
```

Note: `outputJSONError` is NOT used in update.go (it uses `progress.Error()` and direct `json.NewEncoder`). So no outputJSON changes needed here.

---

### Task 7: Migrate cache.go and download.go

**Files:**
- Modify: `cmd/cache.go`
- Modify: `cmd/download.go`

**Step 1: Migrate cache.go**

Update imports: replace `viper` with `clix`. Keep `reporter` (used in runCacheRemove and runCacheClear). Keep `os` (used for `os.Stdout` in direct JSON output, and for reporter).

Actually, check: with `clix.NewReporter()` the `os` import may not be needed for reporter creation. But `os` may still be needed for other things in cache.go. Check each function.

In `runCacheList`:
- `viper.GetBool("json")` → `clix.JSONOutput`
- `outputJSONError(...)` → `clix.OutputJSONError(...)`
- `return outputJSON(output)` → `clix.OutputJSON(output); return nil`

In `runCacheRemove`:
- `viper.GetBool("json")` → `clix.JSONOutput`
- Replace manual reporter creation with `clix.NewReporter()`
- `outputJSONError(...)` → `clix.OutputJSONError(...)`
- `_ = outputJSON(...)` → `clix.OutputJSON(...)`

In `runCacheClear`:
- Same pattern as runCacheRemove

**Step 2: Migrate download.go**

Update imports: replace `viper` with `clix`. Keep `reporter` if still needed for type reference.

In `runDownload`:
- `viper.GetBool("json")` → `clix.JSONOutput`
- `viper.GetBool("verbose")` → `clix.Verbose`
- `viper.GetBool("dry-run")` → `clix.DryRun`
- Replace both reporter creation blocks with `clix.NewReporter()`
- `outputJSONError(...)` → `clix.OutputJSONError(...)`
- `return outputJSON(output)` → `clix.OutputJSON(output); return nil`

Check if `reporter` import can be removed (if only used for `reporter.Reporter` type and `reporter.New*Reporter()` calls that are now replaced by clix).

---

### Task 8: Migrate lint.go and interactive_install.go

**Files:**
- Modify: `cmd/lint.go`
- Modify: `cmd/interactive_install.go`

**Step 1: Migrate lint.go**

Update imports: replace `viper` with `clix`. Keep `encoding/json` (lint.go has direct JSON encoding for certain output).

In `runLint`:
- `viper.GetBool("verbose")` → `clix.Verbose`
- `viper.GetBool("json")` → `clix.JSONOutput`
- `outputJSONError(...)` → `clix.OutputJSONError(...)`
- Any `outputJSON(...)` → `clix.OutputJSON(...)`
- Direct `json.MarshalIndent` usage stays as-is (command-specific formatting)

**Step 2: Migrate interactive_install.go**

Update imports: replace `viper` with `clix`.

In `runInteractiveInstall`:
- `viper.GetBool("verbose")` → `clix.Verbose`
- `viper.GetBool("dry-run")` → `clix.DryRun`

No reporter or JSON output changes needed (interactive mode doesn't use them).

---

### Task 9: Check gendocs.go

**Files:**
- Modify: `cmd/gendocs.go` (if it references `rootCmd`)

**Step 1: Check for rootCmd references**

If gendocs.go references `rootCmd` (e.g., in init or command registration), update to `RootCmd`. If it doesn't use viper or reporter, no other changes needed.

---

### Task 10: Clean up and verify

**Files:**
- Modify: `go.mod` / `go.sum`

**Step 1: Remove unused viper dependency**

Run: `go mod tidy`

This will remove `github.com/spf13/viper` if no code imports it anymore. It may remain as a transitive dependency of clix — that's fine.

**Step 2: Build**

Run: `make build`
Expected: Success. Binary builds with version injection.

**Step 3: Run unit tests**

Run: `make test-unit`
Expected: All tests pass.

**Step 4: Run linter**

Run: `make lint`
Expected: No new lint issues.

**Step 5: Verify version output**

Run: `./nbc --version`
Expected: Shows version string in format: `nbc version dev (Commit: none) (Date: unknown) (Built by: local)`

**Step 6: Verify flags exist**

Run: `./nbc --help`
Expected: Shows `--verbose/-v`, `--dry-run/-n`, `--json`, `--silent/-s` in global flags.

**Step 7: Commit**

```bash
git add -A
git commit -m "refactor: replace hand-rolled CLI infrastructure with clix

Migrates version injection, common flags (verbose, dry-run, json),
reporter factory, and JSON output helpers to github.com/frostyard/clix.

Behavioral changes:
- Text progress output now goes to stderr (Unix convention)
- New --silent/-s flag suppresses all progress output
- Removes direct viper dependency from cmd/"
```

---

### Task 11: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update Console Output section**

Replace the Reporter bullet point to mention clix:

```markdown
### Console Output
- Use the `Reporter` interface for all user-facing output — never raw `fmt.Print` in `pkg/`
- Create reporters via `clix.NewReporter()` in `cmd/` — handles JSON/text/silent mode
- Access common flags via `clix.Verbose`, `clix.DryRun`, `clix.JSONOutput`, `clix.Silent`
- Use `clix.OutputJSON()` and `clix.OutputJSONError()` for structured JSON output
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for clix migration"
```
