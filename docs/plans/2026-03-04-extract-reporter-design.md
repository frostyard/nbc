# Extract Reporter to External Package

## Summary

Replace the local Reporter implementation in `pkg/reporter.go` with the extracted
`github.com/frostyard/std/reporter` package. The external package has an identical
API surface, making this a mechanical import swap with no behavior changes.

## Changes

### New dependency

`github.com/frostyard/std` — contains the reporter package with identical
Reporter interface, TextReporter, JSONReporter, and NoopReporter implementations.

### Files deleted

- `pkg/reporter.go` — interface + 3 implementations (moved to external package)
- `pkg/reporter_test.go` — tests live in the external package now

### Files edited

- `pkg/types/types.go` — remove ProgressEvent, EventType, and EventType constants
  (lines 20-47); these now live in `reporter.ProgressEvent` and `reporter.EventType`
- All files importing `pkg.Reporter` or related types — update to use
  `github.com/frostyard/std/reporter` package
- `pkg/workflow_test.go` — update `stepTrackingReporter` to embed
  `reporter.NoopReporter`

### What stays the same

- All business logic unchanged
- Reporter interface contract identical
- Other types in `pkg/types/types.go` (Lint, Cache, Status, etc.) untouched
- No behavior changes

## Approach

Direct replacement (no aliases, no gradual migration). The APIs match exactly,
so every reference is a simple import swap.
