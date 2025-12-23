package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
//    func CheckMyThing(rootDir string) []LintIssue {
//        var issues []LintIssue
//
//        // rootDir is the extracted filesystem root (e.g., "/tmp/nbc-lint-xxx")
//        // or "/" when running with --local inside a container
//
//        path := filepath.Join(rootDir, "path", "to", "check")
//        if _, err := os.Stat(path); err == nil {
//            issues = append(issues, LintIssue{
//                Check:    "my-thing",        // Unique identifier for this check
//                Severity: SeverityError,     // or SeverityWarning
//                Message:  "Description of the problem and how to fix it",
//                Path:     "/path/to/check",  // Optional: file path (display only)
//            })
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
// SEVERITY LEVELS
// ---------------
//
// - SeverityError: Causes non-zero exit code. Use for issues that will
//   definitely cause problems (e.g., shared SSH keys, non-empty machine-id).
//
// - SeverityWarning: Does not cause non-zero exit. Use for issues that
//   might cause problems or are best-practice violations.
//
// =============================================================================

// LintSeverity represents the severity of a lint issue
type LintSeverity string

const (
	SeverityError   LintSeverity = "error"
	SeverityWarning LintSeverity = "warning"
)

// LintIssue represents a single lint issue found
type LintIssue struct {
	Check    string       `json:"check"`
	Severity LintSeverity `json:"severity"`
	Message  string       `json:"message"`
	Path     string       `json:"path,omitempty"`
}

// LintResult contains all issues found by the linter
type LintResult struct {
	Issues     []LintIssue `json:"issues"`
	ErrorCount int         `json:"error_count"`
	WarnCount  int         `json:"warning_count"`
}

// LintCheck is a function that performs a lint check on a container filesystem
// It returns a list of issues found
type LintCheck func(rootDir string) []LintIssue

// Linter performs lint checks on container images
type Linter struct {
	checks  []LintCheck
	verbose bool
	quiet   bool // Suppress all output (for JSON mode)
}

// NewLinter creates a new Linter with default checks
func NewLinter() *Linter {
	l := &Linter{}
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
		issues := check(rootDir)
		for _, issue := range issues {
			result.Issues = append(result.Issues, issue)
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

	// In quiet mode, redirect stdout to discard extraction messages
	var oldStdout *os.File
	if l.quiet {
		oldStdout = os.Stdout
		os.Stdout, _ = os.Open(os.DevNull)
	}

	if l.verbose && !l.quiet {
		fmt.Printf("Extracting image to %s...\n", tmpDir)
	}

	// Extract the container image
	extractor := NewContainerExtractor(imageRef, tmpDir)
	extractor.SetVerbose(l.verbose && !l.quiet)
	err = extractor.Extract()

	// Restore stdout
	if l.quiet && oldStdout != nil {
		os.Stdout = oldStdout
	}

	if err != nil {
		return nil, fmt.Errorf("failed to extract container image: %w", err)
	}

	if l.verbose && !l.quiet {
		fmt.Println("Running lint checks...")
	}

	return l.Lint(tmpDir), nil
}

// =============================================================================
// Built-in Lint Checks
// =============================================================================

// CheckSSHHostKeys checks for SSH host keys that should not be in the image
func CheckSSHHostKeys(rootDir string) []LintIssue {
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
			issues = append(issues, LintIssue{
				Check:    "ssh-host-keys",
				Severity: SeverityError,
				Message:  "SSH host key found in image - these should be generated at first boot, not baked into the image",
				Path:     "/" + relPath,
			})
		}
	}

	return issues
}

// CheckMachineID checks for a non-empty machine-id
func CheckMachineID(rootDir string) []LintIssue {
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
				issues = append(issues, LintIssue{
					Check:    "machine-id",
					Severity: SeverityError,
					Message:  fmt.Sprintf("machine-id contains a value (%s) - should be empty or 'uninitialized' for container images", contentStr[:min(8, len(contentStr))]+"..."),
					Path:     "/etc/machine-id",
				})
			}
		}
	}

	return issues
}

// CheckRandomSeed checks for random seed files that should not be in the image
func CheckRandomSeed(rootDir string) []LintIssue {
	var issues []LintIssue

	// Files that contain random data and should not be shared
	seedFiles := []string{
		"var/lib/systemd/random-seed",
		"var/lib/random-seed",
	}

	for _, seedFile := range seedFiles {
		path := filepath.Join(rootDir, seedFile)
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			issues = append(issues, LintIssue{
				Check:    "random-seed",
				Severity: SeverityWarning,
				Message:  "Random seed file found - this should not be shared across systems",
				Path:     "/" + seedFile,
			})
		}
	}

	return issues
}
