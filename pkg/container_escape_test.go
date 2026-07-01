package pkg

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTar_SymlinkEscapeDoesNotWriteOutsideRoot verifies that a malicious
// layer cannot write outside the extraction root by planting a symlink that
// points outside the root and then writing a file "through" it.
//
// Attack: entry 1 is a symlink "escape" -> <absolute path outside targetDir>;
// entry 2 is a regular file "escape/pwned". A naive extractor calls
// os.OpenFile("<targetDir>/escape/pwned"), the OS follows the symlink, and the
// file lands OUTSIDE targetDir (arbitrary host write as root).
func TestExtractTar_SymlinkEscapeDoesNotWriteOutsideRoot(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "rootfs")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "host" location outside the extraction root that the attacker targets.
	hostDir := filepath.Join(root, "host")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "escape",
		Typeflag: tar.TypeSymlink,
		Linkname: hostDir, // absolute path outside targetDir
		Mode:     0o777,
	}); err != nil {
		t.Fatal(err)
	}
	content := []byte("owned")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "escape/pwned",
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o644,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	// Extraction may legitimately error on the hostile entry; the security
	// property is that nothing is written outside targetDir.
	_ = extractTar(t.Context(), bytes.NewReader(buf.Bytes()), targetDir)

	escaped := filepath.Join(hostDir, "pwned")
	if _, err := os.Lstat(escaped); !os.IsNotExist(err) {
		t.Fatalf("symlink escape: a file was written outside the extraction root at %s", escaped)
	}
}

// TestExtractTar_HardlinkEscapeDoesNotDiscloseHostFile verifies that a malicious
// layer cannot pull a file from outside the extraction root into the image via a
// hard link whose target escapes the root with "..".
//
// Attack: a hard-link entry "loot" -> "../secret.txt". A naive extractor joins
// the link name onto targetDir (escaping via ..), os.Link fails with EXDEV
// across devices, and the fallback copies the host file's contents into the
// image (arbitrary host file disclosure).
func TestExtractTar_HardlinkEscapeDoesNotDiscloseHostFile(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "rootfs")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-HOST-DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "loot",
		Typeflag: tar.TypeLink,
		Linkname: "../secret.txt", // escapes targetDir
		Mode:     0o644,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	_ = extractTar(t.Context(), bytes.NewReader(buf.Bytes()), targetDir)

	loot := filepath.Join(targetDir, "loot")
	if data, err := os.ReadFile(loot); err == nil {
		if strings.Contains(string(data), "TOP-SECRET-HOST-DATA") {
			t.Fatalf("hardlink escape: host secret was disclosed into the image at %s", loot)
		}
	}
}

// TestExtractTar_LegitimateSymlinksPreserved is a regression guard: the escape
// fix must NOT break normal container symlinks, including symlinks whose target
// is an absolute path (extremely common in real rootfs images, e.g.
// /etc/localtime -> /usr/share/zoneinfo/...). The link target must be stored
// verbatim.
func TestExtractTar_LegitimateSymlinksPreserved(t *testing.T) {
	targetDir := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// Parent directories, as a real image would include.
	for _, d := range []string{"usr/", "usr/lib/", "etc/"} {
		if err := tw.WriteHeader(&tar.Header{Name: d, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	links := []struct{ name, target string }{
		{"usr/lib/rel", "../share/thing"},            // relative target
		{"etc/localtime", "/usr/share/zoneinfo/UTC"}, // absolute target (legit)
	}
	for _, l := range links {
		if err := tw.WriteHeader(&tar.Header{
			Name:     l.name,
			Typeflag: tar.TypeSymlink,
			Linkname: l.target,
			Mode:     0o777,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractTar(t.Context(), bytes.NewReader(buf.Bytes()), targetDir); err != nil {
		t.Fatalf("extractTar failed on legitimate symlinks: %v", err)
	}

	for _, l := range links {
		got, err := os.Readlink(filepath.Join(targetDir, l.name))
		if err != nil {
			t.Fatalf("readlink %s: %v", l.name, err)
		}
		if got != l.target {
			t.Errorf("symlink %s target = %q, want %q", l.name, got, l.target)
		}
	}
}
