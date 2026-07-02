package pkg

import "testing"

func countArg(args []string, want string) int {
	n := 0
	for _, a := range args {
		if a == want {
			n++
		}
	}
	return n
}

// TestBuildGRUBCmdline_NoDuplicateRO is the regression test for #100. The base
// kernel cmdline already contains "ro" at index 1, and generateGRUBConfig also
// prepends "ro" explicitly; the old filter only dropped "rw", so the generated
// grub.cfg ended up with "root=... ro console=tty0 ro ...". The GRUB cmdline
// must contain "ro" exactly once.
func TestBuildGRUBCmdline_NoDuplicateRO(t *testing.T) {
	in := []string{"root=UUID=abc", "ro", "rd.luks.uuid=x", "quiet"}
	got := buildGRUBCmdline(in)

	if n := countArg(got, "ro"); n != 1 {
		t.Errorf("expected exactly one 'ro', got %d in %v", n, got)
	}
	if len(got) < 3 || got[0] != "root=UUID=abc" || got[1] != "ro" || got[2] != "console=tty0" {
		t.Errorf("expected prefix [root=..., ro, console=tty0], got %v", got)
	}
	// The remaining args must be preserved.
	if countArg(got, "rd.luks.uuid=x") != 1 || countArg(got, "quiet") != 1 {
		t.Errorf("expected remaining args preserved, got %v", got)
	}
}

// TestBuildGRUBCmdline_DropsRW verifies "rw" is stripped (GRUB boots read-only).
func TestBuildGRUBCmdline_DropsRW(t *testing.T) {
	in := []string{"root=UUID=abc", "rw", "quiet"}
	got := buildGRUBCmdline(in)
	if countArg(got, "rw") != 0 {
		t.Errorf("'rw' should be dropped, got %v", got)
	}
	if countArg(got, "ro") != 1 {
		t.Errorf("expected exactly one 'ro', got %v", got)
	}
}

// TestBuildGRUBCmdline_EmptyReturnsNil ensures an empty base cmdline does not
// produce a rootless (unbootable) command line; the caller is expected to reject
// it before writing grub.cfg.
func TestBuildGRUBCmdline_EmptyReturnsNil(t *testing.T) {
	if got := buildGRUBCmdline(nil); got != nil {
		t.Errorf("empty input must return nil, got %v", got)
	}
	if got := buildGRUBCmdline([]string{}); got != nil {
		t.Errorf("empty slice must return nil, got %v", got)
	}
}
