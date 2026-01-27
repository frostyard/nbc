// Package testutil provides test helpers and fixtures for nbc testing.
package testutil

import (
	"regexp"
	"testing"

	"github.com/sebdah/goldie/v2"
)

// NewGolden creates a configured goldie instance for golden file testing.
// Uses testdata directory with .golden suffix and colored diff output.
//
// Usage:
//
//	func TestCLI_List(t *testing.T) {
//	    output := runCommand("nbc", "list")
//	    g := testutil.NewGolden(t)
//	    g.Assert(t, "list-output", []byte(output))
//	}
//
// Update golden files: go test -update ./...
func NewGolden(t *testing.T) *goldie.Goldie {
	t.Helper()
	return goldie.New(t,
		goldie.WithFixtureDir("testdata"),
		goldie.WithNameSuffix(".golden"),
		goldie.WithDiffEngine(goldie.ColoredDiff),
	)
}

// NormalizeOutput normalizes dynamic content in output before golden file comparison.
// This ensures tests are deterministic despite timestamps, UUIDs, and paths.
//
// Normalizations applied:
//   - ISO timestamps (2024-01-15T10:30:45Z) -> TIMESTAMP
//   - UUIDs (8-4-4-4-12 format) -> UUID
//   - Loop devices (/dev/loop0) -> /dev/loopN
//   - Temp paths (/tmp/...) -> /tmp/...
//
// Structure and ordering are preserved.
func NormalizeOutput(s string) string {
	// Replace ISO timestamps: 2024-01-15T10:30:45Z or 2024-01-15T10:30:45+00:00
	timestampRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[Z+-][\d:]*`)
	s = timestampRe.ReplaceAllString(s, "TIMESTAMP")

	// Replace date-only format: 2024-01-15 (not followed by T for timestamp)
	// We already replaced timestamps above, so standalone dates like "2024-01-15 " or end-of-line are safe.
	// Match dates at word boundary (space, comma, end of line, etc)
	dateRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}(\s|,|$|[^T0-9])`)
	s = dateRe.ReplaceAllStringFunc(s, func(match string) string {
		// Preserve the trailing character (space, comma, etc.) if present
		if len(match) > 10 {
			return "DATE" + match[10:]
		}
		return "DATE"
	})

	// Replace UUIDs: 8-4-4-4-12 format (lowercase hex)
	uuidRe := regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)
	s = uuidRe.ReplaceAllString(s, "UUID")

	// Replace uppercase UUIDs as well
	uuidUpperRe := regexp.MustCompile(`[A-F0-9]{8}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{12}`)
	s = uuidUpperRe.ReplaceAllString(s, "UUID")

	// Replace loop devices: /dev/loop0 -> /dev/loopN
	loopRe := regexp.MustCompile(`/dev/loop\d+`)
	s = loopRe.ReplaceAllString(s, "/dev/loopN")

	// Replace temp directory paths with numbers: /tmp/xxx123 -> /tmp/...
	// This catches Go's t.TempDir() style paths
	tmpRe := regexp.MustCompile(`/tmp/[a-zA-Z0-9._-]+`)
	s = tmpRe.ReplaceAllString(s, "/tmp/...")

	// Replace PIDs in common patterns: (pid=12345) -> (pid=PID)
	pidRe := regexp.MustCompile(`\bpid[=:]\s*\d+`)
	s = pidRe.ReplaceAllString(s, "pid=PID")

	// Replace version strings: v0.14.0-25-g5568b48 -> VERSION
	// Matches semantic versions with optional pre-release and git suffix
	versionRe := regexp.MustCompile(`v\d+\.\d+\.\d+(-\d+-g[a-f0-9]+)?`)
	s = versionRe.ReplaceAllString(s, "VERSION")

	return s
}

// AssertGolden is a convenience wrapper that normalizes output and asserts against golden file.
// It combines NormalizeOutput and goldie assertion in one call.
//
// Usage:
//
//	func TestCLI_Status(t *testing.T) {
//	    output := runCommand("nbc", "status")
//	    testutil.AssertGolden(t, "status-output", []byte(output))
//	}
func AssertGolden(t *testing.T, name string, actual []byte) {
	t.Helper()

	// Normalize the output for deterministic comparison
	normalized := NormalizeOutput(string(actual))

	// Create goldie instance and assert
	g := NewGolden(t)
	g.Assert(t, name, []byte(normalized))
}

// AssertGoldenString is like AssertGolden but takes a string instead of bytes.
func AssertGoldenString(t *testing.T, name string, actual string) {
	t.Helper()
	AssertGolden(t, name, []byte(actual))
}
