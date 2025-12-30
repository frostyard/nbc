package pkg

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTar_PreservesSpecialBits(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges to set SUID/SGID bits")
	}

	// Create a test directory
	targetDir := t.TempDir()

	// Create a tar archive with files that have special permission bits
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	tests := []struct {
		name     string
		path     string
		mode     int64
		expected os.FileMode
	}{
		{
			name:     "SUID binary",
			path:     "bin/sudo",
			mode:     04755, // SUID + rwxr-xr-x
			expected: os.FileMode(0755) | os.ModeSetuid,
		},
		{
			name:     "SGID binary",
			path:     "usr/bin/wall",
			mode:     02755, // SGID + rwxr-xr-x
			expected: os.FileMode(0755) | os.ModeSetgid,
		},
		{
			name:     "SUID+SGID binary",
			path:     "bin/pkexec",
			mode:     06755, // SUID+SGID + rwxr-xr-x
			expected: os.FileMode(0755) | os.ModeSetuid | os.ModeSetgid,
		},
		{
			name:     "Sticky directory",
			path:     "tmp",
			mode:     01777, // Sticky + rwxrwxrwx
			expected: os.FileMode(0777) | os.ModeSticky,
		},
		{
			name:     "Regular file",
			path:     "etc/passwd",
			mode:     0644,
			expected: os.FileMode(0644),
		},
	}

	for _, tt := range tests {
		var header tar.Header
		if tt.path == "tmp" {
			// Directory
			header = tar.Header{
				Name:     tt.path,
				Mode:     tt.mode,
				Typeflag: tar.TypeDir,
				Uid:      0,
				Gid:      0,
			}
		} else {
			// Regular file with some content
			content := []byte("test content for " + tt.name)
			header = tar.Header{
				Name:     tt.path,
				Mode:     tt.mode,
				Size:     int64(len(content)),
				Typeflag: tar.TypeReg,
				Uid:      0,
				Gid:      0,
			}
			if err := tw.WriteHeader(&header); err != nil {
				t.Fatalf("Failed to write tar header for %s: %v", tt.name, err)
			}
			if _, err := tw.Write(content); err != nil {
				t.Fatalf("Failed to write tar content for %s: %v", tt.name, err)
			}
			continue
		}

		if err := tw.WriteHeader(&header); err != nil {
			t.Fatalf("Failed to write tar header for %s: %v", tt.name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	// Extract the tar
	reader := bytes.NewReader(buf.Bytes())
	if err := extractTar(reader, targetDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	// Verify that special bits are preserved
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(targetDir, tt.path)

			info, err := os.Lstat(filePath)
			if err != nil {
				t.Fatalf("Failed to stat %s: %v", filePath, err)
			}

			actualMode := info.Mode()

			// Check permission bits
			expectedPerm := tt.expected & os.ModePerm
			actualPerm := actualMode & os.ModePerm
			if actualPerm != expectedPerm {
				t.Errorf("Permission bits mismatch for %s: got %04o, want %04o",
					tt.path, actualPerm, expectedPerm)
			}

			// Check SUID bit
			expectedSUID := tt.expected&os.ModeSetuid != 0
			actualSUID := actualMode&os.ModeSetuid != 0
			if actualSUID != expectedSUID {
				t.Errorf("SUID bit mismatch for %s: got %v, want %v",
					tt.path, actualSUID, expectedSUID)
			}

			// Check SGID bit
			expectedSGID := tt.expected&os.ModeSetgid != 0
			actualSGID := actualMode&os.ModeSetgid != 0
			if actualSGID != expectedSGID {
				t.Errorf("SGID bit mismatch for %s: got %v, want %v",
					tt.path, actualSGID, expectedSGID)
			}

			// Check Sticky bit
			expectedSticky := tt.expected&os.ModeSticky != 0
			actualSticky := actualMode&os.ModeSticky != 0
			if actualSticky != expectedSticky {
				t.Errorf("Sticky bit mismatch for %s: got %v, want %v",
					tt.path, actualSticky, expectedSticky)
			}

			// Log the actual mode for debugging
			t.Logf("File: %s, Mode: %04o (SUID=%v SGID=%v Sticky=%v)",
				tt.path, actualMode&os.ModePerm, actualSUID, actualSGID, actualSticky)
		})
	}
}

func TestExtractTar_WhiteoutHandling(t *testing.T) {
	targetDir := t.TempDir()

	// Create initial files
	testFile := filepath.Join(targetDir, "dir", "file.txt")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create tar with whiteout
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Regular whiteout - should delete file.txt
	if err := tw.WriteHeader(&tar.Header{
		Name:     "dir/.wh.file.txt",
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("Failed to write whiteout header: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	// Extract the tar
	reader := bytes.NewReader(buf.Bytes())
	if err := extractTar(reader, targetDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("Whiteout file should have been deleted: %s", testFile)
	}
}

func TestExtractTar_OpaqueWhiteout(t *testing.T) {
	targetDir := t.TempDir()

	// Create initial files in a directory
	testDir := filepath.Join(targetDir, "dir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		if err := os.WriteFile(filepath.Join(testDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create tar with opaque whiteout
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Opaque whiteout - should delete all files in dir
	if err := tw.WriteHeader(&tar.Header{
		Name:     "dir/.wh..wh..opq",
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("Failed to write opaque whiteout header: %v", err)
	}

	// Add a new file after the whiteout
	newFile := "dir/newfile.txt"
	content := []byte("new content")
	if err := tw.WriteHeader(&tar.Header{
		Name:     newFile,
		Mode:     0644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("Failed to write new file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Failed to write new file content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	// Extract the tar
	reader := bytes.NewReader(buf.Bytes())
	if err := extractTar(reader, targetDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	// Verify old files were deleted
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		oldFile := filepath.Join(testDir, name)
		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			t.Errorf("Opaque whiteout should have deleted file: %s", oldFile)
		}
	}

	// Verify new file exists
	newFilePath := filepath.Join(targetDir, newFile)
	if _, err := os.Stat(newFilePath); err != nil {
		t.Errorf("New file should exist after opaque whiteout: %s", newFilePath)
	}
}

func TestVerifyExtraction_ValidFilesystem(t *testing.T) {
	targetDir := t.TempDir()

	// Create minimal valid Linux filesystem structure
	dirs := []string{
		"usr",
		"usr/bin",
		"usr/lib",
		"etc",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create os-release file (required)
	osReleasePath := filepath.Join(targetDir, "usr/lib/os-release")
	if err := os.WriteFile(osReleasePath, []byte("ID=test\nNAME=Test Linux\n"), 0644); err != nil {
		t.Fatalf("Failed to create os-release: %v", err)
	}

	// Create enough content to pass size check (100MB+)
	// Create a large file to simulate actual content
	largeFile := filepath.Join(targetDir, "usr/lib/large_lib.so")
	data := make([]byte, 110*1024*1024) // 110MB
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	err := VerifyExtraction(targetDir)
	if err != nil {
		t.Errorf("VerifyExtraction should pass for valid filesystem: %v", err)
	}
}

func TestVerifyExtraction_MissingDirectory(t *testing.T) {
	targetDir := t.TempDir()

	// Create only some directories (missing usr/bin)
	dirs := []string{
		"usr",
		"usr/lib",
		"etc",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	err := VerifyExtraction(targetDir)
	if err == nil {
		t.Error("VerifyExtraction should fail when required directory is missing")
	}
}

func TestVerifyExtraction_MissingOsRelease(t *testing.T) {
	targetDir := t.TempDir()

	// Create directories but no os-release
	dirs := []string{
		"usr",
		"usr/bin",
		"usr/lib",
		"etc",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	err := VerifyExtraction(targetDir)
	if err == nil {
		t.Error("VerifyExtraction should fail when os-release is missing")
	}
}

func TestVerifyExtraction_TooSmall(t *testing.T) {
	targetDir := t.TempDir()

	// Create valid structure but with minimal content
	dirs := []string{
		"usr",
		"usr/bin",
		"usr/lib",
		"etc",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create os-release but no other content
	osReleasePath := filepath.Join(targetDir, "usr/lib/os-release")
	if err := os.WriteFile(osReleasePath, []byte("ID=test\n"), 0644); err != nil {
		t.Fatalf("Failed to create os-release: %v", err)
	}

	err := VerifyExtraction(targetDir)
	if err == nil {
		t.Error("VerifyExtraction should fail when filesystem is too small")
	}
}

func TestVerifyExtraction_EmptyDirectory(t *testing.T) {
	targetDir := t.TempDir()

	// Empty directory simulates the failed extraction scenario
	err := VerifyExtraction(targetDir)
	if err == nil {
		t.Error("VerifyExtraction should fail for empty directory")
	}
}

func TestSetupSystemDirectories(t *testing.T) {
	targetDir := t.TempDir()

	err := SetupSystemDirectories(targetDir)
	if err != nil {
		t.Fatalf("SetupSystemDirectories failed: %v", err)
	}

	// Check all expected directories exist
	expectedDirs := []string{
		"dev",
		"proc",
		"sys",
		"run",
		"tmp",
		"var/tmp",
		"boot",
		// Note: .etc.lower is now created by PopulateEtcLower(), not SetupSystemDirectories()
		"mnt",
		"media",
		"opt",
		"srv",
	}

	for _, dir := range expectedDirs {
		path := filepath.Join(targetDir, dir)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", dir)
			continue
		}
		if err != nil {
			t.Errorf("error checking directory %s: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", dir)
		}
	}

	// Check tmp directories have sticky bit
	tmpInfo, _ := os.Stat(filepath.Join(targetDir, "tmp"))
	if tmpInfo.Mode()&os.ModeSticky == 0 {
		t.Error("tmp directory should have sticky bit set")
	}

	varTmpInfo, _ := os.Stat(filepath.Join(targetDir, "var", "tmp"))
	if varTmpInfo.Mode()&os.ModeSticky == 0 {
		t.Error("var/tmp directory should have sticky bit set")
	}
}

func TestPrepareMachineID(t *testing.T) {
	t.Run("creates uninitialized when file does not exist", func(t *testing.T) {
		targetDir := t.TempDir()
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}

		err := PrepareMachineID(targetDir)
		if err != nil {
			t.Fatalf("PrepareMachineID failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(etcDir, "machine-id"))
		if err != nil {
			t.Fatalf("failed to read machine-id: %v", err)
		}

		if string(content) != "uninitialized\n" {
			t.Errorf("machine-id = %q, want %q", string(content), "uninitialized\n")
		}
	})

	t.Run("sets uninitialized when file is empty", func(t *testing.T) {
		targetDir := t.TempDir()
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte(""), 0444); err != nil {
			t.Fatalf("failed to create empty machine-id: %v", err)
		}

		err := PrepareMachineID(targetDir)
		if err != nil {
			t.Fatalf("PrepareMachineID failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(etcDir, "machine-id"))
		if err != nil {
			t.Fatalf("failed to read machine-id: %v", err)
		}

		if string(content) != "uninitialized\n" {
			t.Errorf("machine-id = %q, want %q", string(content), "uninitialized\n")
		}
	})

	t.Run("preserves existing uninitialized", func(t *testing.T) {
		targetDir := t.TempDir()
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte("uninitialized\n"), 0444); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		err := PrepareMachineID(targetDir)
		if err != nil {
			t.Fatalf("PrepareMachineID failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(etcDir, "machine-id"))
		if err != nil {
			t.Fatalf("failed to read machine-id: %v", err)
		}

		if string(content) != "uninitialized\n" {
			t.Errorf("machine-id = %q, want %q", string(content), "uninitialized\n")
		}
	})

	t.Run("leaves existing real machine-id unchanged", func(t *testing.T) {
		targetDir := t.TempDir()
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		realID := "abc123def456abc123def456abc123de\n"
		if err := os.WriteFile(filepath.Join(etcDir, "machine-id"), []byte(realID), 0444); err != nil {
			t.Fatalf("failed to create machine-id: %v", err)
		}

		err := PrepareMachineID(targetDir)
		if err != nil {
			t.Fatalf("PrepareMachineID failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(etcDir, "machine-id"))
		if err != nil {
			t.Fatalf("failed to read machine-id: %v", err)
		}

		// Should be unchanged (with warning printed)
		if string(content) != realID {
			t.Errorf("machine-id = %q, want %q (should be unchanged)", string(content), realID)
		}
	})
}
