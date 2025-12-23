package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// LintOutput represents the JSON output structure for the lint command
type LintOutput struct {
	Image      string          `json:"image,omitempty"`
	Local      bool            `json:"local,omitempty"`
	Issues     []pkg.LintIssue `json:"issues"`
	ErrorCount int             `json:"error_count"`
	WarnCount  int             `json:"warning_count"`
	Success    bool            `json:"success"`
}

var lintLocal bool

var lintCmd = &cobra.Command{
	Use:   "lint [image]",
	Short: "Check a container image for common issues",
	Long: `Lint a container image for common issues that may cause problems
when installed with nbc.

Checks include:
  - SSH host keys (should not be baked into images)
  - machine-id (should be empty or 'uninitialized')
  - Random seed files (should not be shared)

Exit codes:
  0 - No errors found (warnings are allowed)
  1 - One or more errors found

Use --local to run inside a container build (e.g., as the last step in a
Dockerfile) to check the current filesystem instead of pulling an image.

Examples:
  # Lint a remote image
  nbc lint ghcr.io/myorg/myimage:latest
  nbc lint --json docker.io/library/fedora:latest

  # Lint the current filesystem (inside a container build)
  nbc lint --local

  # In a Dockerfile:
  # RUN nbc lint --local`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLint,
}

func init() {
	rootCmd.AddCommand(lintCmd)
	lintCmd.Flags().BoolVar(&lintLocal, "local", false, "Lint the current filesystem instead of a container image (for use inside container builds)")
}

func runLint(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	jsonOutput := viper.GetBool("json")

	// Validate arguments
	if lintLocal && len(args) > 0 {
		return fmt.Errorf("cannot specify both --local and an image reference")
	}
	if !lintLocal && len(args) == 0 {
		return fmt.Errorf("image reference required (or use --local to lint current filesystem)")
	}

	linter := pkg.NewLinter()
	linter.SetVerbose(verbose)
	linter.SetQuiet(jsonOutput) // Suppress stdout for clean JSON output

	var result *pkg.LintResult
	var err error
	var imageRef string

	if lintLocal {
		// Lint the current filesystem (for use inside container builds)
		if !jsonOutput {
			fmt.Println("Linting current filesystem...")
		}
		result = linter.Lint("/")
	} else {
		// Lint a container image
		imageRef = args[0]
		if !jsonOutput {
			fmt.Printf("Linting container image: %s\n", imageRef)
		}
		result, err = linter.LintContainerImage(imageRef)
		if err != nil {
			if jsonOutput {
				output := LintOutput{
					Image:   imageRef,
					Success: false,
					Issues: []pkg.LintIssue{{
						Check:    "extraction",
						Severity: pkg.SeverityError,
						Message:  err.Error(),
					}},
					ErrorCount: 1,
				}
				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Println(string(data))
				os.Exit(1)
			}
			return fmt.Errorf("failed to lint image: %w", err)
		}
	}

	if jsonOutput {
		output := LintOutput{
			Issues:     result.Issues,
			ErrorCount: result.ErrorCount,
			WarnCount:  result.WarnCount,
			Success:    result.ErrorCount == 0,
		}
		if lintLocal {
			output.Local = true
		} else {
			output.Image = imageRef
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
		if result.ErrorCount > 0 {
			os.Exit(1)
		}
		return nil
	}

	// Plain text output
	if len(result.Issues) == 0 {
		fmt.Println("\n✓ No issues found")
		return nil
	}

	fmt.Println("\nIssues found:")
	for _, issue := range result.Issues {
		var prefix string
		switch issue.Severity {
		case pkg.SeverityError:
			prefix = "ERROR"
		case pkg.SeverityWarning:
			prefix = "WARN "
		}

		if issue.Path != "" {
			fmt.Printf("  [%s] %s: %s\n", prefix, issue.Path, issue.Message)
		} else {
			fmt.Printf("  [%s] %s\n", prefix, issue.Message)
		}
	}

	fmt.Printf("\nSummary: %d error(s), %d warning(s)\n", result.ErrorCount, result.WarnCount)

	if result.ErrorCount > 0 {
		fmt.Println("\nTo fix SSH host key issues, add this to your Dockerfile:")
		fmt.Println("  RUN rm -f /etc/ssh/ssh_host_*")
		fmt.Println("\nTo fix machine-id issues, add this to your Dockerfile:")
		fmt.Println("  RUN truncate -s 0 /etc/machine-id")
		os.Exit(1)
	}

	return nil
}
