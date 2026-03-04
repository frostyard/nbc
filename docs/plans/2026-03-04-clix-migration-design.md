# Design: Replace CLI Infrastructure with github.com/frostyard/clix

## Goal

Replace hand-rolled CLI infrastructure (version injection, flag registration, viper binding, reporter factory, JSON output helpers) with `github.com/frostyard/clix` for standardization across frostyard projects.

## Approach

Full adoption in a single pass. All changes are in `main.go` and `cmd/` — business logic in `pkg/` is unaffected.

## Architecture

```
main.go → clix.App.Run(rootCmd) → cobra + fang
cmd/*.go → clix.Verbose, clix.DryRun, clix.JSONOutput, clix.Silent
cmd/*.go → clix.NewReporter(), clix.OutputJSON(), clix.OutputJSONError()
```

Viper is removed from `cmd/` entirely. clix package-level variables replace all `viper.GetBool()` calls.

## Changes by File

### main.go

Replace version setter calls with `clix.App` struct. Replace `cmd.Execute()` with `app.Run(cmd.RootCmd)`.

### cmd/root.go

Remove:
- Version setter functions and variables
- `makeVersionString()`
- `Execute()` function
- Flag registration in `init()`
- Viper imports and binding

Add:
- Export `rootCmd` as `RootCmd`

### cmd/install.go, cmd/download.go, cmd/cache.go, cmd/validate.go, cmd/lint.go, cmd/status.go, cmd/list.go, cmd/interactive_install.go, cmd/gendocs.go

- Replace `viper.GetBool("verbose")` → `clix.Verbose`
- Replace `viper.GetBool("dry-run")` → `clix.DryRun`
- Replace `viper.GetBool("json")` → `clix.JSONOutput`
- Replace manual reporter creation → `clix.NewReporter()`
- Replace `outputJSON()` → `clix.OutputJSON()`
- Replace `outputJSONError()` → `clix.OutputJSONError()`

### cmd/update.go

Same as above, except: keep manual `NoopReporter` override for the `--check --json` case (command-specific logic that doesn't belong in clix).

## Behavioral Changes

1. **Text output moves from stdout to stderr** — aligns with Unix convention (stdout for data, stderr for progress).
2. **New `--silent/-s` flag** — suppresses all output. Additive feature.
3. **`clix.OutputJSON()` returns bool** — callers adjust accordingly (returns true if JSON was written).

## What Does Not Change

- All `pkg/` code remains untouched
- Flag structs per command (installFlags, updateFlags, etc.) stay as-is
- Command registration pattern (init() + AddCommand) stays
- Error handling patterns unchanged
- Test helpers in `pkg/testutil/` unchanged

## Dependencies

- Add: `github.com/frostyard/clix`
- Remove: direct `github.com/spf13/viper` usage from cmd/ (may still be transitive)
- No new transitive dependencies (clix uses same libraries already in nbc)
