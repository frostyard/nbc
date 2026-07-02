package pkg

import (
	"regexp"
	"testing"
)

// TestDracutModuleDeclaresCryptDependency is the regression test for #98. The
// 95etc-overlay module must declare the crypt dracut module in depends() so it
// is force-included in the initramfs even for generic (non-hostonly) image
// builds; otherwise an encrypted /var cannot be unlocked before the overlay is
// mounted. We assert the *shipped* (embedded) module, since that is what nbc
// installs. The check targets the echoed dependency line, not the explanatory
// comment (which also mentions crypt).
func TestDracutModuleDeclaresCryptDependency(t *testing.T) {
	content, err := dracutModuleFS.ReadFile("dracut/95etc-overlay/module-setup.sh")
	if err != nil {
		t.Fatalf("read embedded module-setup.sh: %v", err)
	}

	echoCrypt := regexp.MustCompile(`(?m)^\s*echo\s+"[^"]*\bcrypt\b[^"]*"`)
	if !echoCrypt.Match(content) {
		t.Errorf("module-setup.sh depends() must echo the 'crypt' dependency (#98)")
	}
}
