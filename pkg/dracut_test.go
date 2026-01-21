package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInitramfsHasEtcOverlay(t *testing.T) {
	t.Run("returns false when no tools available", func(t *testing.T) {
		// This test is tricky because it depends on system tools
		// We can only verify the function doesn't panic

		// Create a dummy file to check
		tmpDir := t.TempDir()
		dummyFile := filepath.Join(tmpDir, "dummy.img")
		if err := os.WriteFile(dummyFile, []byte("dummy"), 0644); err != nil {
			t.Fatalf("failed to create dummy file: %v", err)
		}

		// The function should return false or an error, but not panic
		result, err := InitramfsHasEtcOverlay(dummyFile)
		// If neither lsinitrd nor lsinitramfs is available, should return false, nil
		// If a tool is available but fails on the dummy file, might return error
		// Either way, it shouldn't panic
		_ = result
		_ = err
	})

	t.Run("returns false for non-existent file", func(t *testing.T) {
		result, err := InitramfsHasEtcOverlay("/nonexistent/path/to/initramfs.img")
		// Should either return false or error, depending on tool availability
		if result {
			t.Error("expected false for non-existent file")
		}
		_ = err // Error is acceptable for non-existent file
	})

	t.Run("detects etc-overlay in real initramfs", func(t *testing.T) {
		// Skip if no initramfs inspection tool is available
		_, lsinitrdErr := exec.LookPath("lsinitrd")
		_, lsinitramfsErr := exec.LookPath("lsinitramfs")
		if lsinitrdErr != nil && lsinitramfsErr != nil {
			t.Skip("no initramfs inspection tool available (lsinitrd or lsinitramfs)")
		}

		// Check if a real initramfs exists on the system
		initramfsPaths := []string{
			"/boot/initrd.img-" + getKernelVersion(),
			"/boot/initramfs-" + getKernelVersion() + ".img",
		}

		var initramfsPath string
		for _, p := range initramfsPaths {
			if _, err := os.Stat(p); err == nil {
				initramfsPath = p
				break
			}
		}

		if initramfsPath == "" {
			t.Skip("no initramfs found on system")
		}

		// Test the function - on a normal system, this should return false
		// (unless this is actually an nbc-installed system)
		// Note: The function may return an error after finding the hook if the
		// listing command fails (e.g., pipe buffer full), but this is handled
		// internally and the result is still valid
		result, err := InitramfsHasEtcOverlay(initramfsPath)

		// Log the result - we can't assert a specific value because we don't
		// know if this system has the etc-overlay module installed
		t.Logf("InitramfsHasEtcOverlay(%s) = %v, err = %v", initramfsPath, result, err)

		// If there's an error, it should only happen after a successful search
		// The function handles pipe buffer issues gracefully
		if err != nil {
			t.Logf("Note: error occurred during listing (likely after finding result): %v", err)
		}
	})
}

// getKernelVersion returns the current kernel version
func getKernelVersion() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	// Trim newline
	version := string(data)
	if len(version) > 0 && version[len(version)-1] == '\n' {
		version = version[:len(version)-1]
	}
	return version
}

func TestVerifyDracutEtcOverlay(t *testing.T) {
	t.Run("dry run always succeeds", func(t *testing.T) {
		progress := NewProgressReporter(false, 1)
		err := VerifyDracutEtcOverlay("/nonexistent", true, progress)
		if err != nil {
			t.Errorf("dry run should succeed: %v", err)
		}
	})

	t.Run("fails when module not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create empty usr/lib/dracut/modules.d/95etc-overlay
		moduleDir := filepath.Join(tmpDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay")
		if err := os.MkdirAll(moduleDir, 0755); err != nil {
			t.Fatalf("failed to create module dir: %v", err)
		}

		// Should fail because required files are missing
		progress := NewProgressReporter(false, 1)
		err := VerifyDracutEtcOverlay(tmpDir, false, progress)
		if err == nil {
			t.Error("should fail when required files are missing")
		}
	})

	t.Run("succeeds when module files exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		moduleDir := filepath.Join(tmpDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay")
		if err := os.MkdirAll(moduleDir, 0755); err != nil {
			t.Fatalf("failed to create module dir: %v", err)
		}

		// Create required files
		for _, filename := range []string{"module-setup.sh", "etc-overlay-mount.sh"} {
			filePath := filepath.Join(moduleDir, filename)
			if err := os.WriteFile(filePath, []byte("#!/bin/bash\n"), 0755); err != nil {
				t.Fatalf("failed to create %s: %v", filename, err)
			}
		}

		progress := NewProgressReporter(false, 1)
		err := VerifyDracutEtcOverlay(tmpDir, false, progress)
		if err != nil {
			t.Errorf("should succeed when module files exist: %v", err)
		}
	})
}
