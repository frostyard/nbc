package pkg

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

// TestCLI_HelpOutput tests the main help output format.
// Uses golden file comparison to detect unintentional changes.
// Run with -update flag to regenerate: go test -update ./pkg/... -run TestCLI_HelpOutput
func TestCLI_HelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run() // Ignore exit code, help may exit 0 or 1

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "help", []byte(output))
}

// TestCLI_ListHelpOutput tests the list subcommand help output.
func TestCLI_ListHelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "list", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "list-help", []byte(output))
}

// TestCLI_InstallHelpOutput tests the install subcommand help output.
func TestCLI_InstallHelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "install", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "install-help", []byte(output))
}

// TestCLI_UpdateHelpOutput tests the update subcommand help output.
func TestCLI_UpdateHelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "update", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "update-help", []byte(output))
}

// TestCLI_VersionOutput tests the version output format.
func TestCLI_VersionOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "version", []byte(output))
}

// TestCLI_StatusHelpOutput tests the status subcommand help output.
func TestCLI_StatusHelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "status", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "status-help", []byte(output))
}

// TestCLI_ValidateHelpOutput tests the validate subcommand help output.
func TestCLI_ValidateHelpOutput(t *testing.T) {
	cmd := exec.Command("./nbc", "validate", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	output := testutil.NormalizeOutput(stdout.String())
	testutil.AssertGolden(t, "validate-help", []byte(output))
}
