package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLUKSConfig(t *testing.T) {
	t.Run("struct initialization", func(t *testing.T) {
		config := &LUKSConfig{
			Enabled:    true,
			Passphrase: "secret",
			TPM2:       false,
		}

		if !config.Enabled {
			t.Error("Enabled should be true")
		}
		if config.Passphrase != "secret" {
			t.Errorf("Passphrase = %q, want %q", config.Passphrase, "secret")
		}
		if config.TPM2 {
			t.Error("TPM2 should be false")
		}
	})

	t.Run("keyfile config", func(t *testing.T) {
		config := &LUKSConfig{
			Enabled: true,
			Keyfile: "/path/to/keyfile",
		}

		if config.Keyfile != "/path/to/keyfile" {
			t.Errorf("Keyfile = %q, want %q", config.Keyfile, "/path/to/keyfile")
		}
	})
}

func TestLUKSDevice(t *testing.T) {
	t.Run("struct initialization", func(t *testing.T) {
		dev := &LUKSDevice{
			Partition:  "/dev/sda2",
			MapperName: "root1",
			MapperPath: "/dev/mapper/root1",
			LUKSUUID:   "12345678-1234-1234-1234-123456789abc",
		}

		if dev.Partition != "/dev/sda2" {
			t.Errorf("Partition = %q, want %q", dev.Partition, "/dev/sda2")
		}
		if dev.MapperName != "root1" {
			t.Errorf("MapperName = %q, want %q", dev.MapperName, "root1")
		}
		if dev.MapperPath != "/dev/mapper/root1" {
			t.Errorf("MapperPath = %q, want %q", dev.MapperPath, "/dev/mapper/root1")
		}
	})
}

func TestValidateInitramfsSupport(t *testing.T) {
	t.Run("warns without LUKS support", func(t *testing.T) {
		targetDir := t.TempDir()

		warnings := ValidateInitramfsSupport(targetDir, false)

		if len(warnings) == 0 {
			t.Error("should return warning when no LUKS support detected")
		}

		// Check warning mentions LUKS
		hasLUKSWarning := false
		for _, w := range warnings {
			if strings.Contains(w, "LUKS") || strings.Contains(w, "crypt") {
				hasLUKSWarning = true
				break
			}
		}
		if !hasLUKSWarning {
			t.Error("warning should mention LUKS support")
		}
	})

	t.Run("no warning with dracut crypt module", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create dracut crypt module directory
		cryptDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "90crypt")
		if err := os.MkdirAll(cryptDir, 0755); err != nil {
			t.Fatalf("failed to create crypt dir: %v", err)
		}

		warnings := ValidateInitramfsSupport(targetDir, false)

		for _, w := range warnings {
			if strings.Contains(w, "LUKS initramfs support") {
				t.Error("should not warn when dracut crypt module exists")
			}
		}
	})

	t.Run("no warning with initramfs-tools cryptroot", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create initramfs-tools cryptroot hook
		cryptHook := filepath.Join(targetDir, "usr", "share", "initramfs-tools", "hooks", "cryptroot")
		if err := os.MkdirAll(filepath.Dir(cryptHook), 0755); err != nil {
			t.Fatalf("failed to create hooks dir: %v", err)
		}
		if err := os.WriteFile(cryptHook, []byte("#!/bin/sh"), 0755); err != nil {
			t.Fatalf("failed to create cryptroot hook: %v", err)
		}

		warnings := ValidateInitramfsSupport(targetDir, false)

		for _, w := range warnings {
			if strings.Contains(w, "LUKS initramfs support") {
				t.Error("should not warn when initramfs-tools cryptroot exists")
			}
		}
	})

	t.Run("warns about TPM2 when enabled but missing", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create LUKS support but not TPM2
		cryptDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "90crypt")
		if err := os.MkdirAll(cryptDir, 0755); err != nil {
			t.Fatalf("failed to create crypt dir: %v", err)
		}

		warnings := ValidateInitramfsSupport(targetDir, true)

		hasTPM2Warning := false
		for _, w := range warnings {
			if strings.Contains(w, "TPM2") {
				hasTPM2Warning = true
				break
			}
		}
		if !hasTPM2Warning {
			t.Error("should warn about missing TPM2 support")
		}
	})

	t.Run("no TPM2 warning when dracut module exists", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create both LUKS and TPM2 dracut modules
		cryptDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "90crypt")
		tpm2Dir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "91tpm2-tss")
		for _, d := range []string{cryptDir, tpm2Dir} {
			if err := os.MkdirAll(d, 0755); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}
		}

		warnings := ValidateInitramfsSupport(targetDir, true)

		for _, w := range warnings {
			if strings.Contains(w, "TPM2 initramfs support") {
				t.Error("should not warn when TPM2 module exists")
			}
		}
	})
}

func TestGenerateCrypttab(t *testing.T) {
	t.Run("generates basic crypttab", func(t *testing.T) {
		devices := []*LUKSDevice{
			{
				MapperName: "root1",
				LUKSUUID:   "uuid-root1",
			},
			{
				MapperName: "var",
				LUKSUUID:   "uuid-var",
			},
		}

		crypttab := GenerateCrypttab(devices, false)

		// Verify header
		if !strings.Contains(crypttab, "# /etc/crypttab") {
			t.Error("crypttab should contain header comment")
		}

		// Verify root1 entry
		if !strings.Contains(crypttab, "root1 UUID=uuid-root1 none luks") {
			t.Error("crypttab should contain root1 entry")
		}

		// Verify var entry
		if !strings.Contains(crypttab, "var UUID=uuid-var none luks") {
			t.Error("crypttab should contain var entry")
		}

		// Verify no TPM2 option when disabled
		if strings.Contains(crypttab, "tpm2-device") {
			t.Error("crypttab should not contain tpm2-device when TPM2 disabled")
		}
	})

	t.Run("generates crypttab with TPM2", func(t *testing.T) {
		devices := []*LUKSDevice{
			{
				MapperName: "root1",
				LUKSUUID:   "uuid-root1",
			},
		}

		crypttab := GenerateCrypttab(devices, true)

		// Verify TPM2 option present
		if !strings.Contains(crypttab, "tpm2-device=auto") {
			t.Error("crypttab should contain tpm2-device=auto when TPM2 enabled")
		}

		// Verify full line format
		if !strings.Contains(crypttab, "root1 UUID=uuid-root1 none luks,tpm2-device=auto") {
			t.Error("crypttab entry format incorrect")
		}
	})

	t.Run("handles empty device list", func(t *testing.T) {
		crypttab := GenerateCrypttab([]*LUKSDevice{}, false)

		// Should still have header
		if !strings.Contains(crypttab, "# /etc/crypttab") {
			t.Error("crypttab should contain header even with no devices")
		}

		// Should not have device entries
		lines := strings.Split(crypttab, "\n")
		for _, line := range lines {
			if line != "" && !strings.HasPrefix(line, "#") {
				t.Errorf("should not have non-comment lines, got: %q", line)
			}
		}
	})

	t.Run("ends with newline", func(t *testing.T) {
		devices := []*LUKSDevice{{MapperName: "test", LUKSUUID: "uuid"}}
		crypttab := GenerateCrypttab(devices, false)

		if !strings.HasSuffix(crypttab, "\n") {
			t.Error("crypttab should end with newline")
		}
	})
}

func TestIsTPMAvailable(t *testing.T) {
	t.Run("returns boolean", func(t *testing.T) {
		// Just verify the function runs without error
		// We can't test the actual result since it depends on the test system
		result := IsTPMAvailable()

		// The result should be either true or false
		if result {
			t.Log("TPM device detected on test system")
		} else {
			t.Log("No TPM device detected on test system")
		}
	})

}
