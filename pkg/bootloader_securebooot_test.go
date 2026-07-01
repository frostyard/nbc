package pkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/std/reporter"
)

// TestSetupSecureBootChain_GRUB2_DoesNotInstallFbx64 is the regression test for
// #89. On the GRUB2 Secure Boot path, fbx64.efi must NOT be installed: like the
// systemd-boot path, our layout uses EFI/BOOT/ directly with no BOOTX64.CSV, so
// the fallback loader triggers a "Restore Boot Option" blue screen. CLAUDE.md
// also states explicitly: do NOT include fbx64.efi.
func TestSetupSecureBootChain_GRUB2_DoesNotInstallFbx64(t *testing.T) {
	targetDir := t.TempDir()

	// Lay out a container rootfs with the binaries the GRUB2 chain looks for.
	writeFile := func(rel string) {
		p := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("EFI-BINARY:"+rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join("usr", "lib", "shim", "shimx64.efi.signed"))                    // findShimEFI
	writeFile(filepath.Join("usr", "lib", "grub", "x86_64-efi-signed", "grubx64.efi.signed")) // findSignedGrubEFI
	writeFile(filepath.Join("usr", "lib", "shim", "mmx64.efi"))                             // findMokManager
	writeFile(filepath.Join("usr", "lib", "shim", "fbx64.efi"))                             // fallback loader (must be ignored)

	b := &BootloaderInstaller{
		Type:      BootloaderGRUB2,
		TargetDir: targetDir,
		Progress:  reporter.NoopReporter{},
	}

	ok, err := b.setupSecureBootChain(filepath.Join(targetDir, "unused-grub.efi"))
	if err != nil {
		t.Fatalf("setupSecureBootChain: %v", err)
	}
	if !ok {
		t.Fatalf("expected Secure Boot chain to be set up (shim present)")
	}

	efiBootDir := filepath.Join(targetDir, "boot", "EFI", "BOOT")

	// The chain itself must be present.
	for _, name := range []string{"BOOTX64.EFI", "grubx64.efi"} {
		if _, err := os.Stat(filepath.Join(efiBootDir, name)); err != nil {
			t.Errorf("expected %s to be installed: %v", name, err)
		}
	}

	// fbx64.efi must NOT be present.
	if _, err := os.Stat(filepath.Join(efiBootDir, "fbx64.efi")); !os.IsNotExist(err) {
		t.Errorf("fbx64.efi must not be installed on the GRUB2 Secure Boot path (causes Restore Boot Option blue screen)")
	}
}
