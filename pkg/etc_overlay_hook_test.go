package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// hookPath returns the path to the dracut etc-overlay hook script relative to
// this package directory.
func hookPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("dracut", "95etc-overlay", "etc-overlay-mount.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("hook script not found at %s: %v", p, err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("failed to resolve hook path: %v", err)
	}
	return abs
}

// runPrepareEtcLower sources the real hook with ETC_OVERLAY_TEST=1 (which makes
// it define its functions and return before running the boot-time flow), then
// invokes prepare_etc_lower() against the provided sandbox sysroot. It returns
// the function's exit status via the error.
func runPrepareEtcLower(t *testing.T, sysroot string) error {
	t.Helper()
	hook := hookPath(t)
	// Stub the dracut helpers the hook expects, point SYSROOT/ETC_LOWER at the
	// sandbox, source the hook (returns at the ETC_OVERLAY_TEST guard), then run
	// only the lower-layer reconciliation logic.
	script := `
set -e
getarg() { :; }
getargbool() { return 1; }
info() { :; }
warn() { :; }
export ETC_OVERLAY_TEST=1
SYSROOT="$1"
. "$2"
SYSROOT="$1"
ETC_LOWER="$SYSROOT/etc"
prepare_etc_lower
`
	cmd := exec.Command("sh", "-c", script, "sh", sysroot, hook)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("prepare_etc_lower output: %s", string(out))
	}
	return err
}

func countEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", dir, err)
	}
	return len(entries)
}

// newOverlaySandbox creates a fake sysroot with a populated /etc and the
// overlay upper/work directories, and returns the sysroot path.
func newOverlaySandbox(t *testing.T) string {
	t.Helper()
	sysroot := t.TempDir()
	etc := filepath.Join(sysroot, "etc")
	upper := filepath.Join(sysroot, "var", "lib", "nbc", "etc-overlay", "upper")
	work := filepath.Join(sysroot, "var", "lib", "nbc", "etc-overlay", "work")
	for _, d := range []string{etc, upper, work} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Populate the container's /etc with some default files.
	for name, content := range map[string]string{
		"os-release":   "ID=snow\n",
		"adduser.conf": "DSHELL=/bin/bash\n",
		"sshd_config":  "PermitRootLogin no\n",
	} {
		if err := os.WriteFile(filepath.Join(etc, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return sysroot
}

// TestEtcOverlayHook_PopulatedLowerDoesNotPolluteUpper is the regression test
// for the defeated-/etc-overlay bug (issue #84). When nbc has pre-populated
// /.etc.lower (Design X), the hook must use it as the lower layer WITHOUT
// copying the root filesystem's /etc into the writable upper layer. A polluted
// upper permanently shadows the lower, so container /etc updates would be
// silently lost on every A/B update.
func TestEtcOverlayHook_PopulatedLowerDoesNotPolluteUpper(t *testing.T) {
	sysroot := newOverlaySandbox(t)
	etc := filepath.Join(sysroot, "etc")
	lower := filepath.Join(sysroot, ".etc.lower")
	upper := filepath.Join(sysroot, "var", "lib", "nbc", "etc-overlay", "upper")

	// Simulate nbc's PopulateEtcLower: .etc.lower is a copy of the container /etc.
	if err := os.MkdirAll(lower, 0755); err != nil {
		t.Fatalf("mkdir lower: %v", err)
	}
	if out, err := exec.Command("cp", "-a", etc+"/.", lower+"/").CombinedOutput(); err != nil {
		t.Fatalf("seed .etc.lower: %v: %s", err, out)
	}

	if got := countEntries(t, upper); got != 0 {
		t.Fatalf("precondition: upper should start empty, has %d entries", got)
	}

	if err := runPrepareEtcLower(t, sysroot); err != nil {
		t.Fatalf("prepare_etc_lower failed: %v", err)
	}

	// The core assertion: upper must remain empty. On the buggy hook it gets the
	// entire /etc copied into it.
	if got := countEntries(t, upper); got != 0 {
		names, _ := os.ReadDir(upper)
		var polluted []string
		for _, n := range names {
			polluted = append(polluted, n.Name())
		}
		t.Errorf("upper layer was polluted with %d entries from /etc: %v", got, polluted)
	}

	// The populated lower layer must be left intact for use as the overlay lowerdir.
	if got := countEntries(t, lower); got == 0 {
		t.Errorf(".etc.lower should remain populated, is empty")
	}
}

// TestEtcOverlayHook_EmptyLowerSeedsFromEtc verifies the fallback path: when
// /.etc.lower is empty/missing, the hook seeds it by moving the root fs /etc,
// and still leaves upper empty.
func TestEtcOverlayHook_EmptyLowerSeedsFromEtc(t *testing.T) {
	sysroot := newOverlaySandbox(t)
	etc := filepath.Join(sysroot, "etc")
	lower := filepath.Join(sysroot, ".etc.lower")
	upper := filepath.Join(sysroot, "var", "lib", "nbc", "etc-overlay", "upper")

	etcEntries := countEntries(t, etc)

	if err := runPrepareEtcLower(t, sysroot); err != nil {
		t.Fatalf("prepare_etc_lower failed: %v", err)
	}

	if got := countEntries(t, upper); got != 0 {
		t.Errorf("upper layer must stay empty, has %d entries", got)
	}
	if got := countEntries(t, lower); got != etcEntries {
		t.Errorf(".etc.lower should have been seeded with %d entries from /etc, has %d", etcEntries, got)
	}
}
