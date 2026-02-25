package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPopulateEtcLower(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, targetDir string)
		wantErr    bool
		errContain string
		verify     func(t *testing.T, targetDir string)
	}{
		{
			name: "successful population",
			setup: func(t *testing.T, targetDir string) {
				// Create /etc with some files
				etcDir := filepath.Join(targetDir, "etc")
				if err := os.MkdirAll(etcDir, 0755); err != nil {
					t.Fatal(err)
				}
				files := map[string]string{
					"passwd":     "root:x:0:0:root:/root:/bin/bash\n",
					"group":      "root:x:0:\n",
					"os-release": "NAME=Test\n",
				}
				for name, content := range files {
					path := filepath.Join(etcDir, name)
					if err := os.WriteFile(path, []byte(content), 0644); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantErr: false,
			verify: func(t *testing.T, targetDir string) {
				etcLower := filepath.Join(targetDir, ".etc.lower")
				// Check directory exists
				info, err := os.Stat(etcLower)
				if err != nil {
					t.Errorf("/.etc.lower does not exist: %v", err)
					return
				}
				if !info.IsDir() {
					t.Error("/.etc.lower is not a directory")
				}
				// Check files were copied
				files := []string{"passwd", "group", "os-release"}
				for _, file := range files {
					path := filepath.Join(etcLower, file)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						t.Errorf("File %s missing in /.etc.lower", file)
					}
				}
				// Verify content
				content, err := os.ReadFile(filepath.Join(etcLower, "passwd"))
				if err != nil {
					t.Errorf("Failed to read passwd from .etc.lower: %v", err)
				}
				if string(content) != "root:x:0:0:root:/root:/bin/bash\n" {
					t.Errorf("passwd content mismatch in .etc.lower")
				}
			},
		},
		{
			name: "missing /etc directory",
			setup: func(t *testing.T, targetDir string) {
				// Don't create /etc
			},
			wantErr:    true,
			errContain: "/etc does not exist",
		},
		{
			name: "empty /etc directory",
			setup: func(t *testing.T, targetDir string) {
				etcDir := filepath.Join(targetDir, "etc")
				if err := os.MkdirAll(etcDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr:    true,
			errContain: "/etc is empty",
		},
		{
			name: "subdirectories in /etc",
			setup: func(t *testing.T, targetDir string) {
				etcDir := filepath.Join(targetDir, "etc")
				if err := os.MkdirAll(filepath.Join(etcDir, "ssh"), 0755); err != nil {
					t.Fatal(err)
				}
				// Create file in subdirectory
				keyPath := filepath.Join(etcDir, "ssh", "ssh_host_rsa_key")
				if err := os.WriteFile(keyPath, []byte("fake-key"), 0600); err != nil {
					t.Fatal(err)
				}
				// Create top-level file
				if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: false,
			verify: func(t *testing.T, targetDir string) {
				etcLower := filepath.Join(targetDir, ".etc.lower")
				// Check subdirectory was copied
				sshDir := filepath.Join(etcLower, "ssh")
				info, err := os.Stat(sshDir)
				if err != nil {
					t.Errorf("ssh subdirectory missing in .etc.lower: %v", err)
					return
				}
				if !info.IsDir() {
					t.Error("ssh is not a directory in .etc.lower")
				}
				// Check file in subdirectory
				keyPath := filepath.Join(sshDir, "ssh_host_rsa_key")
				content, err := os.ReadFile(keyPath)
				if err != nil {
					t.Errorf("ssh_host_rsa_key missing in .etc.lower: %v", err)
					return
				}
				if string(content) != "fake-key" {
					t.Error("ssh_host_rsa_key content mismatch")
				}
				// Verify permissions preserved
				keyInfo, err := os.Stat(keyPath)
				if err != nil {
					t.Fatal(err)
				}
				if keyInfo.Mode().Perm() != 0600 {
					t.Errorf("permissions not preserved: got %o, want 0600", keyInfo.Mode().Perm())
				}
			},
		},
		{
			name: "symlinks in /etc",
			setup: func(t *testing.T, targetDir string) {
				etcDir := filepath.Join(targetDir, "etc")
				if err := os.MkdirAll(etcDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Create a file
				targetFile := filepath.Join(etcDir, "localtime.real")
				if err := os.WriteFile(targetFile, []byte("timezone-data"), 0644); err != nil {
					t.Fatal(err)
				}
				// Create a symlink
				linkPath := filepath.Join(etcDir, "localtime")
				if err := os.Symlink("localtime.real", linkPath); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: false,
			verify: func(t *testing.T, targetDir string) {
				etcLower := filepath.Join(targetDir, ".etc.lower")
				// Check symlink was copied
				linkPath := filepath.Join(etcLower, "localtime")
				info, err := os.Lstat(linkPath)
				if err != nil {
					t.Errorf("symlink missing in .etc.lower: %v", err)
					return
				}
				if info.Mode()&os.ModeSymlink == 0 {
					t.Error("localtime is not a symlink in .etc.lower")
				}
				// Check symlink target
				target, err := os.Readlink(linkPath)
				if err != nil {
					t.Errorf("failed to read symlink: %v", err)
				}
				if target != "localtime.real" {
					t.Errorf("symlink target mismatch: got %s, want localtime.real", target)
				}
			},
		},
		{
			name: "dry run mode",
			setup: func(t *testing.T, targetDir string) {
				etcDir := filepath.Join(targetDir, "etc")
				if err := os.MkdirAll(etcDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: false,
			verify: func(t *testing.T, targetDir string) {
				// In dry run mode, .etc.lower should NOT be created
				etcLower := filepath.Join(targetDir, ".etc.lower")
				if _, err := os.Stat(etcLower); !os.IsNotExist(err) {
					t.Error(".etc.lower should not exist in dry run mode")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()

			// Setup test environment
			if tt.setup != nil {
				tt.setup(t, tmpDir)
			}

			// Run the function
			dryRun := tt.name == "dry run mode"
			err := PopulateEtcLower(context.Background(), tmpDir, dryRun, NoopReporter{})

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("PopulateEtcLower() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("PopulateEtcLower() error = %v, want error containing %q", err, tt.errContain)
				}
			}

			// Run verification
			if tt.verify != nil {
				tt.verify(t, tmpDir)
			}
		})
	}
}

func TestPopulateEtcLower_Overwrite(t *testing.T) {
	// Test that PopulateEtcLower overwrites existing .etc.lower content
	tmpDir := t.TempDir()

	// Create initial /etc
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "file1"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create old .etc.lower with different content
	etcLower := filepath.Join(tmpDir, ".etc.lower")
	if err := os.MkdirAll(etcLower, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcLower, "oldfile"), []byte("oldcontent"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run PopulateEtcLower
	if err := PopulateEtcLower(context.Background(), tmpDir, false, NoopReporter{}); err != nil {
		t.Fatalf("PopulateEtcLower() failed: %v", err)
	}

	// Verify new content
	content, err := os.ReadFile(filepath.Join(etcLower, "file1"))
	if err != nil {
		t.Errorf("file1 missing in .etc.lower: %v", err)
	}
	if string(content) != "content1" {
		t.Error("file1 content mismatch")
	}

	// Verify old file was deleted
	if _, err := os.Stat(filepath.Join(etcLower, "oldfile")); !os.IsNotExist(err) {
		t.Error("oldfile should have been deleted from .etc.lower")
	}
}
