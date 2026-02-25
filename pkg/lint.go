package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/nbc/pkg/types"
)

// =============================================================================
// Lint Framework
// =============================================================================
//
// The lint framework provides extensible checking of container images for
// common issues that cause problems when installed with nbc.
//
// ADDING NEW LINT CHECKS
// ----------------------
//
// 1. Create a new check function with the LintCheck signature:
//
//    func CheckMyThing(rootDir string, fix bool) []LintIssue {
//        var issues []LintIssue
//
//        // rootDir is the extracted filesystem root (e.g., "/tmp/nbc-lint-xxx")
//        // or "/" when running with --local inside a container
//
//        path := filepath.Join(rootDir, "path", "to", "check")
//        if _, err := os.Stat(path); err == nil {
//            issue := LintIssue{
//                Check:    "my-thing",        // Unique identifier for this check
//                Severity: SeverityError,     // or SeverityWarning
//                Message:  "Description of the problem and how to fix it",
//                Path:     "/path/to/check",  // Optional: file path (display only)
//            }
//
//            // If fix is true, attempt to remediate the issue
//            if fix {
//                if err := os.Remove(path); err == nil {
//                    issue.Fixed = true
//                    issue.Message = "Problem was automatically fixed"
//                }
//            }
//
//            issues = append(issues, issue)
//        }
//
//        return issues
//    }
//
// 2. Register the check in RegisterDefaultChecks():
//
//    func (l *Linter) RegisterDefaultChecks() {
//        l.RegisterCheck(CheckSSHHostKeys)
//        l.RegisterCheck(CheckMachineID)
//        l.RegisterCheck(CheckRandomSeed)
//        l.RegisterCheck(CheckMyThing)  // Add your check here
//    }
//
// 3. Update docs/LINT.md with:
//    - Check name and severity
//    - What it checks and why it matters
//    - Auto-fix behavior
//    - Manual fix instructions (Dockerfile example)
//
// SEVERITY LEVELS
// ---------------
//
// - SeverityError: Causes non-zero exit code. Use for issues that will
//   definitely cause problems (e.g., shared SSH keys, non-empty machine-id).
//
// - SeverityWarning: Does not cause non-zero exit. Use for issues that
//   might cause problems or are best-practice violations.
//
// AUTO-FIX SUPPORT
// ----------------
//
// When the user runs `nbc lint --local --fix`, the fix parameter is true.
// Checks should attempt to remediate issues and set issue.Fixed = true
// if successful. Fixed issues don't count toward the error/warning totals.
//
// =============================================================================

// Type aliases for backward compatibility
type LintSeverity = types.LintSeverity
type LintIssue = types.LintIssue
type LintResult = types.LintResult

// Re-export constants for backward compatibility
const (
	SeverityError   = types.SeverityError
	SeverityWarning = types.SeverityWarning
)

// LintCheck is a function that performs a lint check on a container filesystem.
// If fix is true, the check should attempt to fix the issue and set Fixed=true on the issue.
// It returns a list of issues found (including fixed ones).
type LintCheck func(rootDir string, fix bool) []LintIssue

// Linter performs lint checks on container images
type Linter struct {
	checks   []LintCheck
	verbose  bool
	quiet    bool // Suppress all output (for JSON mode)
	fix      bool // Attempt to fix issues (only valid with local mode)
	progress Reporter
}

// NewLinter creates a new Linter with default checks
func NewLinter() *Linter {
	l := &Linter{
		progress: NewTextReporter(os.Stdout),
	}
	l.RegisterDefaultChecks()
	return l
}

// SetVerbose enables verbose output
func (l *Linter) SetVerbose(verbose bool) {
	l.verbose = verbose
}

// SetQuiet suppresses all stdout output (for JSON mode)
func (l *Linter) SetQuiet(quiet bool) {
	l.quiet = quiet
	if quiet {
		l.progress = NoopReporter{}
	}
}

// SetFix enables automatic fixing of issues
func (l *Linter) SetFix(fix bool) {
	l.fix = fix
}

// IsRunningInContainer checks if the current process is running inside a container.
// It looks for common marker files created by container runtimes:
//   - /.dockerenv (Docker)
//   - /run/.containerenv (Podman)
//
// This is used as a safety check before applying --fix in --local mode.
func IsRunningInContainer() bool {
	containerMarkers := []string{
		"/.dockerenv",        // Docker
		"/run/.containerenv", // Podman
	}

	for _, marker := range containerMarkers {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}

	return false
}

// RegisterCheck adds a new lint check
func (l *Linter) RegisterCheck(check LintCheck) {
	l.checks = append(l.checks, check)
}

// RegisterDefaultChecks registers all built-in lint checks
func (l *Linter) RegisterDefaultChecks() {
	l.RegisterCheck(CheckSSHHostKeys)
	l.RegisterCheck(CheckMachineID)
	l.RegisterCheck(CheckRandomSeed)
}

// Lint runs all registered checks on the given directory
func (l *Linter) Lint(rootDir string) *LintResult {
	result := &LintResult{
		Issues: []LintIssue{},
	}

	for _, check := range l.checks {
		issues := check(rootDir, l.fix)
		for _, issue := range issues {
			result.Issues = append(result.Issues, issue)
			if issue.Fixed {
				result.FixedCount++
				continue // Don't count fixed issues as errors/warnings
			}
			switch issue.Severity {
			case SeverityError:
				result.ErrorCount++
			case SeverityWarning:
				result.WarnCount++
			}
		}
	}

	return result
}

// LintContainerImage extracts and lints a container image
func (l *Linter) LintContainerImage(imageRef string) (*LintResult, error) {
	// Create temporary directory for extraction
	tmpDir, err := os.MkdirTemp("", "nbc-lint-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if l.verbose {
		l.progress.Message("Extracting image to %s...", tmpDir)
	}

	// Extract the container image
	extractor := NewContainerExtractor(imageRef, tmpDir)
	extractor.SetVerbose(l.verbose && !l.quiet)
	extractor.SetProgress(l.progress)
	if err := extractor.Extract(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to extract container image: %w", err)
	}

	if l.verbose {
		l.progress.Message("Running lint checks...")
	}

	return l.Lint(tmpDir), nil
}

// =============================================================================
// Built-in Lint Checks
// =============================================================================

// CheckSSHHostKeys checks for SSH host keys that should not be in the image
func CheckSSHHostKeys(rootDir string, fix bool) []LintIssue {
	var issues []LintIssue

	sshDir := filepath.Join(rootDir, "etc", "ssh")
	hostKeyPatterns := []string{
		"ssh_host_*_key",
		"ssh_host_*_key.pub",
	}

	for _, pattern := range hostKeyPatterns {
		matches, err := filepath.Glob(filepath.Join(sshDir, pattern))
		if err != nil {
			continue
		}

		for _, match := range matches {
			relPath, _ := filepath.Rel(rootDir, match)
			issue := LintIssue{
				Check:    "ssh-host-keys",
				Severity: SeverityError,
				Message:  "SSH host key found in image - these should be generated at first boot, not baked into the image",
				Path:     "/" + relPath,
			}

			if fix {
				if err := os.Remove(match); err == nil {
					issue.Fixed = true
					issue.Message = "SSH host key removed (will be regenerated at first boot)"
				}
			}

			issues = append(issues, issue)
		}
	}

	return issues
}

// CheckMachineID checks for a non-empty machine-id
func CheckMachineID(rootDir string, fix bool) []LintIssue {
	var issues []LintIssue

	machineIDPath := filepath.Join(rootDir, "etc", "machine-id")

	info, err := os.Stat(machineIDPath)
	if os.IsNotExist(err) {
		// No machine-id is fine - systemd will create it
		return issues
	}
	if err != nil {
		return issues
	}

	// machine-id should either not exist or be empty
	if info.Size() > 0 {
		// Check if it contains "uninitialized" which is acceptable
		content, err := os.ReadFile(machineIDPath)
		if err == nil {
			contentStr := strings.TrimSpace(string(content))
			if contentStr != "" && contentStr != "uninitialized" {
				issue := LintIssue{
					Check:    "machine-id",
					Severity: SeverityError,
					Message:  fmt.Sprintf("machine-id contains a value (%s) - should be empty or 'uninitialized' for container images", contentStr[:min(8, len(contentStr))]+"..."),
					Path:     "/etc/machine-id",
				}

				if fix {
					// Truncate the file to zero length
					if err := os.Truncate(machineIDPath, 0); err == nil {
						issue.Fixed = true
						issue.Message = "machine-id truncated to empty (will be regenerated at first boot)"
					}
				}

				issues = append(issues, issue)
			}
		}
	}

	return issues
}

// CheckRandomSeed checks for random seed files that should not be in the image
func CheckRandomSeed(rootDir string, fix bool) []LintIssue {
	var issues []LintIssue

	// Files that contain random data and should not be shared
	seedFiles := []string{
		"var/lib/systemd/random-seed",
		"var/lib/random-seed",
	}

	for _, seedFile := range seedFiles {
		path := filepath.Join(rootDir, seedFile)
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			issue := LintIssue{
				Check:    "random-seed",
				Severity: SeverityWarning,
				Message:  "Random seed file found - this should not be shared across systems",
				Path:     "/" + seedFile,
			}

			if fix {
				if err := os.Remove(path); err == nil {
					issue.Fixed = true
					issue.Message = "Random seed file removed (will be regenerated at boot)"
				}
			}

			issues = append(issues, issue)
		}
	}

	return issues
}
