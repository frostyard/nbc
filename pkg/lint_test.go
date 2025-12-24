package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLintSeverityConstants(t *testing.T) {
	t.Run("SeverityError is error", func(t *testing.T) {
		if SeverityError != "error" {
			t.Errorf("SeverityError = %q, want %q", SeverityError, "error")
		}
	})

	t.Run("SeverityWarning is warning", func(t *testing.T) {
		if SeverityWarning != "warning" {
			t.Errorf("SeverityWarning = %q, want %q", SeverityWarning, "warning")
		}
	})
}

func TestNewLinter(t *testing.T) {
	t.Run("creates linter with default checks", func(t *testing.T) {
		linter := NewLinter()
		if linter == nil {
			t.Fatal("NewLinter returned nil")
		}
		// Should have 3 default checks registered
		if len(linter.checks) != 3 {
			t.Errorf("NewLinter has %d checks, want 3", len(linter.checks))
		}
	})
}

func TestLinterSetters(t *testing.T) {
	linter := NewLinter()

	t.Run("SetVerbose", func(t *testing.T) {
		linter.SetVerbose(true)
		if !linter.verbose {
			t.Error("SetVerbose(true) did not set verbose to true")
		}
		linter.SetVerbose(false)
		if linter.verbose {
			t.Error("SetVerbose(false) did not set verbose to false")
		}
	})

	t.Run("SetQuiet", func(t *testing.T) {
		linter.SetQuiet(true)
		if !linter.quiet {
			t.Error("SetQuiet(true) did not set quiet to true")
		}
		linter.SetQuiet(false)
		if linter.quiet {
			t.Error("SetQuiet(false) did not set quiet to false")
		}
	})

	t.Run("SetFix", func(t *testing.T) {
		linter.SetFix(true)
		if !linter.fix {
			t.Error("SetFix(true) did not set fix to true")
		}
		linter.SetFix(false)
		if linter.fix {
			t.Error("SetFix(false) did not set fix to false")
		}
	})
}

func TestLinterRegisterCheck(t *testing.T) {
	t.Run("adds custom check", func(t *testing.T) {
		linter := &Linter{} // Empty linter without default checks
		customCheck := func(rootDir string, fix bool) []LintIssue {
			return []LintIssue{{Check: "custom", Severity: SeverityWarning, Message: "test"}}
		}

		linter.RegisterCheck(customCheck)
		if len(linter.checks) != 1 {
			t.Errorf("RegisterCheck did not add check, got %d checks", len(linter.checks))
		}
	})
}

func TestLinterLint(t *testing.T) {
	t.Run("returns empty result for clean directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create minimal directory structure
		if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "ssh"), 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}

		linter := NewLinter()
		result := linter.Lint(tmpDir)

		if len(result.Issues) != 0 {
			t.Errorf("expected no issues for clean directory, got %d", len(result.Issues))
		}
		if result.ErrorCount != 0 {
			t.Errorf("expected 0 errors, got %d", result.ErrorCount)
		}
		if result.WarnCount != 0 {
			t.Errorf("expected 0 warnings, got %d", result.WarnCount)
		}
	})

	t.Run("counts errors and warnings correctly", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create SSH host key (error)
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "ssh_host_rsa_key"), []byte("fake-key"), 0600); err != nil {
			t.Fatalf("failed to create ssh host key: %v", err)
		}

		// Create random seed (warning)
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "random-seed"), []byte("seed"), 0644); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		linter := NewLinter()
		result := linter.Lint(tmpDir)

		if result.ErrorCount != 1 {
			t.Errorf("expected 1 error, got %d", result.ErrorCount)
		}
		if result.WarnCount != 1 {
			t.Errorf("expected 1 warning, got %d", result.WarnCount)
		}
	})

	t.Run("counts fixed issues separately", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create SSH host key
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "ssh_host_rsa_key"), []byte("fake-key"), 0600); err != nil {
			t.Fatalf("failed to create ssh host key: %v", err)
		}

		linter := NewLinter()
		linter.SetFix(true)
		result := linter.Lint(tmpDir)

		if result.ErrorCount != 0 {
			t.Errorf("expected 0 errors (fixed), got %d", result.ErrorCount)
		}
		if result.FixedCount != 1 {
			t.Errorf("expected 1 fixed, got %d", result.FixedCount)
		}
	})
}

func TestCheckSSHHostKeys(t *testing.T) {
	t.Run("no issues when no SSH keys", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "ssh"), 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}

		issues := CheckSSHHostKeys(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues, got %d", len(issues))
		}
	})

	t.Run("detects private SSH host keys", func(t *testing.T) {
		tmpDir := t.TempDir()
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}

		keyTypes := []string{"rsa", "ecdsa", "ed25519"}
		for _, kt := range keyTypes {
			keyPath := filepath.Join(sshDir, "ssh_host_"+kt+"_key")
			if err := os.WriteFile(keyPath, []byte("fake-key"), 0600); err != nil {
				t.Fatalf("failed to create %s: %v", keyPath, err)
			}
		}

		issues := CheckSSHHostKeys(tmpDir, false)
		if len(issues) != 3 {
			t.Errorf("expected 3 issues (one per key type), got %d", len(issues))
		}

		for _, issue := range issues {
			if issue.Check != "ssh-host-keys" {
				t.Errorf("issue.Check = %q, want %q", issue.Check, "ssh-host-keys")
			}
			if issue.Severity != SeverityError {
				t.Errorf("issue.Severity = %q, want %q", issue.Severity, SeverityError)
			}
			if issue.Fixed {
				t.Error("issue should not be marked as fixed when fix=false")
			}
		}
	})

	t.Run("detects public SSH host keys", func(t *testing.T) {
		tmpDir := t.TempDir()
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}

		if err := os.WriteFile(filepath.Join(sshDir, "ssh_host_rsa_key.pub"), []byte("fake-pub-key"), 0644); err != nil {
			t.Fatalf("failed to create public key: %v", err)
		}

		issues := CheckSSHHostKeys(tmpDir, false)
		if len(issues) != 1 {
			t.Errorf("expected 1 issue for public key, got %d", len(issues))
		}
	})

	t.Run("fixes SSH host keys when fix=true", func(t *testing.T) {
		tmpDir := t.TempDir()
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}

		keyPath := filepath.Join(sshDir, "ssh_host_rsa_key")
		if err := os.WriteFile(keyPath, []byte("fake-key"), 0600); err != nil {
			t.Fatalf("failed to create ssh host key: %v", err)
		}

		issues := CheckSSHHostKeys(tmpDir, true)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}

		if !issues[0].Fixed {
			t.Error("issue should be marked as fixed when fix=true")
		}

		// Verify file was actually removed
		if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
			t.Error("SSH key file should have been removed")
		}
	})
}

func TestCheckMachineID(t *testing.T) {
	t.Run("no issues when machine-id does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755); err != nil {
			t.Fatalf("failed to create etc: %v", err)
		}

		issues := CheckMachineID(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues when machine-id missing, got %d", len(issues))
		}
	})

	t.Run("no issues when machine-id is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		etcDir := filepath.Join(tmpDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte(""), 0444); err != nil {
			t.Fatalf("failed to create empty machine-id: %v", err)
		}

		issues := CheckMachineID(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues for empty machine-id, got %d", len(issues))
		}
	})

	t.Run("no issues when machine-id contains uninitialized", func(t *testing.T) {
		tmpDir := t.TempDir()
		etcDir := filepath.Join(tmpDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte("uninitialized\n"), 0444); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		issues := CheckMachineID(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues for 'uninitialized' machine-id, got %d", len(issues))
		}
	})

	t.Run("detects non-empty machine-id", func(t *testing.T) {
		tmpDir := t.TempDir()
		etcDir := filepath.Join(tmpDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc: %v", err)
		}
		machineID := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte(machineID+"\n"), 0444); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		issues := CheckMachineID(tmpDir, false)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue for non-empty machine-id, got %d", len(issues))
		}

		issue := issues[0]
		if issue.Check != "machine-id" {
			t.Errorf("issue.Check = %q, want %q", issue.Check, "machine-id")
		}
		if issue.Severity != SeverityError {
			t.Errorf("issue.Severity = %q, want %q", issue.Severity, SeverityError)
		}
		if issue.Path != "/etc/machine-id" {
			t.Errorf("issue.Path = %q, want %q", issue.Path, "/etc/machine-id")
		}
	})

	t.Run("fixes machine-id when fix=true", func(t *testing.T) {
		tmpDir := t.TempDir()
		etcDir := filepath.Join(tmpDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc: %v", err)
		}
		machineIDPath := filepath.Join(etcDir, "machine-id")
		if err := os.WriteFile(machineIDPath, []byte("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4\n"), 0644); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		issues := CheckMachineID(tmpDir, true)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}

		if !issues[0].Fixed {
			t.Error("issue should be marked as fixed when fix=true")
		}

		// Verify file was truncated
		info, err := os.Stat(machineIDPath)
		if err != nil {
			t.Fatalf("failed to stat machine-id: %v", err)
		}
		if info.Size() != 0 {
			t.Errorf("machine-id should be empty after fix, got size %d", info.Size())
		}
	})
}

func TestCheckRandomSeed(t *testing.T) {
	t.Run("no issues when no seed files exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmpDir, "var", "lib", "systemd"), 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues, got %d", len(issues))
		}
	})

	t.Run("no issues when seed file is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "random-seed"), []byte(""), 0600); err != nil {
			t.Fatalf("failed to create empty random-seed: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, false)
		if len(issues) != 0 {
			t.Errorf("expected no issues for empty seed, got %d", len(issues))
		}
	})

	t.Run("detects systemd random-seed", func(t *testing.T) {
		tmpDir := t.TempDir()
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "random-seed"), []byte("random-data"), 0600); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, false)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}

		issue := issues[0]
		if issue.Check != "random-seed" {
			t.Errorf("issue.Check = %q, want %q", issue.Check, "random-seed")
		}
		if issue.Severity != SeverityWarning {
			t.Errorf("issue.Severity = %q, want %q", issue.Severity, SeverityWarning)
		}
		if issue.Path != "/var/lib/systemd/random-seed" {
			t.Errorf("issue.Path = %q, want %q", issue.Path, "/var/lib/systemd/random-seed")
		}
	})

	t.Run("detects legacy random-seed location", func(t *testing.T) {
		tmpDir := t.TempDir()
		seedDir := filepath.Join(tmpDir, "var", "lib")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib: %v", err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "random-seed"), []byte("random-data"), 0600); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, false)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue for legacy location, got %d", len(issues))
		}
		if issues[0].Path != "/var/lib/random-seed" {
			t.Errorf("issue.Path = %q, want %q", issues[0].Path, "/var/lib/random-seed")
		}
	})

	t.Run("fixes random-seed when fix=true", func(t *testing.T) {
		tmpDir := t.TempDir()
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		seedPath := filepath.Join(seedDir, "random-seed")
		if err := os.WriteFile(seedPath, []byte("random-data"), 0600); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, true)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}

		if !issues[0].Fixed {
			t.Error("issue should be marked as fixed when fix=true")
		}

		// Verify file was removed
		if _, err := os.Stat(seedPath); !os.IsNotExist(err) {
			t.Error("random-seed file should have been removed")
		}
	})

	t.Run("detects both seed file locations", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create both locations
		systemdDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		legacyDir := filepath.Join(tmpDir, "var", "lib")
		if err := os.MkdirAll(systemdDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}

		if err := os.WriteFile(filepath.Join(systemdDir, "random-seed"), []byte("seed1"), 0600); err != nil {
			t.Fatalf("failed to create systemd random-seed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(legacyDir, "random-seed"), []byte("seed2"), 0600); err != nil {
			t.Fatalf("failed to create legacy random-seed: %v", err)
		}

		issues := CheckRandomSeed(tmpDir, false)
		if len(issues) != 2 {
			t.Errorf("expected 2 issues for both locations, got %d", len(issues))
		}
	})
}

func TestLintResult(t *testing.T) {
	t.Run("empty result has zero counts", func(t *testing.T) {
		result := &LintResult{Issues: []LintIssue{}}
		if result.ErrorCount != 0 {
			t.Errorf("ErrorCount = %d, want 0", result.ErrorCount)
		}
		if result.WarnCount != 0 {
			t.Errorf("WarnCount = %d, want 0", result.WarnCount)
		}
		if result.FixedCount != 0 {
			t.Errorf("FixedCount = %d, want 0", result.FixedCount)
		}
	})
}

func TestLintIssue(t *testing.T) {
	t.Run("issue fields are set correctly", func(t *testing.T) {
		issue := LintIssue{
			Check:    "test-check",
			Severity: SeverityError,
			Message:  "test message",
			Path:     "/test/path",
			Fixed:    true,
		}

		if issue.Check != "test-check" {
			t.Errorf("Check = %q, want %q", issue.Check, "test-check")
		}
		if issue.Severity != SeverityError {
			t.Errorf("Severity = %q, want %q", issue.Severity, SeverityError)
		}
		if issue.Message != "test message" {
			t.Errorf("Message = %q, want %q", issue.Message, "test message")
		}
		if issue.Path != "/test/path" {
			t.Errorf("Path = %q, want %q", issue.Path, "/test/path")
		}
		if !issue.Fixed {
			t.Error("Fixed should be true")
		}
	})
}

func TestMultipleIssuesInSameRun(t *testing.T) {
	t.Run("detects multiple issue types", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create SSH key (error)
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "ssh_host_rsa_key"), []byte("key"), 0600); err != nil {
			t.Fatalf("failed to create ssh key: %v", err)
		}

		// Create machine-id (error)
		if err := os.WriteFile(filepath.Join(tmpDir, "etc", "machine-id"), []byte("abc123"), 0644); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		// Create random seed (warning)
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "random-seed"), []byte("seed"), 0600); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		linter := NewLinter()
		result := linter.Lint(tmpDir)

		if result.ErrorCount != 2 {
			t.Errorf("expected 2 errors, got %d", result.ErrorCount)
		}
		if result.WarnCount != 1 {
			t.Errorf("expected 1 warning, got %d", result.WarnCount)
		}
		if len(result.Issues) != 3 {
			t.Errorf("expected 3 issues total, got %d", len(result.Issues))
		}
	})

	t.Run("fixes all issues when fix=true", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create SSH key
		sshDir := filepath.Join(tmpDir, "etc", "ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatalf("failed to create etc/ssh: %v", err)
		}
		sshKeyPath := filepath.Join(sshDir, "ssh_host_rsa_key")
		if err := os.WriteFile(sshKeyPath, []byte("key"), 0600); err != nil {
			t.Fatalf("failed to create ssh key: %v", err)
		}

		// Create machine-id
		machineIDPath := filepath.Join(tmpDir, "etc", "machine-id")
		if err := os.WriteFile(machineIDPath, []byte("abc123"), 0644); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		// Create random seed
		seedDir := filepath.Join(tmpDir, "var", "lib", "systemd")
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatalf("failed to create var/lib/systemd: %v", err)
		}
		seedPath := filepath.Join(seedDir, "random-seed")
		if err := os.WriteFile(seedPath, []byte("seed"), 0600); err != nil {
			t.Fatalf("failed to create random-seed: %v", err)
		}

		linter := NewLinter()
		linter.SetFix(true)
		result := linter.Lint(tmpDir)

		if result.ErrorCount != 0 {
			t.Errorf("expected 0 errors after fix, got %d", result.ErrorCount)
		}
		if result.WarnCount != 0 {
			t.Errorf("expected 0 warnings after fix, got %d", result.WarnCount)
		}
		if result.FixedCount != 3 {
			t.Errorf("expected 3 fixed, got %d", result.FixedCount)
		}

		// Verify files were fixed
		if _, err := os.Stat(sshKeyPath); !os.IsNotExist(err) {
			t.Error("SSH key should have been removed")
		}
		info, err := os.Stat(machineIDPath)
		if err != nil || info.Size() != 0 {
			t.Error("machine-id should be empty")
		}
		if _, err := os.Stat(seedPath); !os.IsNotExist(err) {
			t.Error("random-seed should have been removed")
		}
	})
}

func TestIsRunningInContainer(t *testing.T) {
	// Note: This test checks the function behavior on the current system.
	// The actual result depends on whether we're running in a container or not.
	// The function itself is simple file existence checks, so we mainly
	// verify it doesn't panic and returns a boolean.

	t.Run("returns boolean without error", func(t *testing.T) {
		result := IsRunningInContainer()
		// Just verify it returns a boolean (doesn't panic)
		_ = result
	})

	t.Run("detects docker marker", func(t *testing.T) {
		// We can't easily test this without mocking the filesystem,
		// but we can at least verify the function checks the right paths.
		// The implementation checks /.dockerenv and /run/.containerenv

		// Check if we're actually in a container
		_, dockerErr := os.Stat("/.dockerenv")
		_, podmanErr := os.Stat("/run/.containerenv")

		inContainer := dockerErr == nil || podmanErr == nil
		result := IsRunningInContainer()

		if result != inContainer {
			t.Errorf("IsRunningInContainer() = %v, but marker files indicate %v", result, inContainer)
		}
	})
}
