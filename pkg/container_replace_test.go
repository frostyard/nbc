package pkg

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// tarEntry is a small helper describing one archive entry for these tests.
type tarEntry struct {
	name     string
	typeflag byte
	linkname string
	content  string
	mode     int64
}

func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Linkname: e.linkname,
			Mode:     mode,
		}
		if e.typeflag == tar.TypeReg {
			h.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

// TestExtractTar_RegularFileReplacesSymlinkLeaf verifies OCI replace semantics:
// when an earlier entry creates a symlink "a" and a later entry writes a regular
// file "a", the file must REPLACE the symlink at "a" -- not be written through
// it to the symlink's referent. Resolving the full path with SecureJoin follows
// the leaf symlink and corrupts the referent instead.
func TestExtractTar_RegularFileReplacesSymlinkLeaf(t *testing.T) {
	targetDir := t.TempDir()

	data := buildTar(t, []tarEntry{
		{name: "b", typeflag: tar.TypeReg, content: "B-ORIGINAL"},
		{name: "a", typeflag: tar.TypeSymlink, linkname: "b", mode: 0o777},
		// later entry replaces the symlink "a" with a regular file
		{name: "a", typeflag: tar.TypeReg, content: "A-CONTENT"},
	})

	if err := extractTar(t.Context(), bytes.NewReader(data), targetDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	aPath := filepath.Join(targetDir, "a")
	if isSymlink(t, aPath) {
		t.Errorf("%s is still a symlink; the regular-file entry should have replaced it", aPath)
	}
	if got, _ := os.ReadFile(aPath); string(got) != "A-CONTENT" {
		t.Errorf("a content = %q, want %q", got, "A-CONTENT")
	}
	if got, _ := os.ReadFile(filepath.Join(targetDir, "b")); string(got) != "B-ORIGINAL" {
		t.Errorf("referent b was corrupted: content = %q, want %q", got, "B-ORIGINAL")
	}
}

// TestExtractTar_AbsoluteSymlinkLeafReplacedNotFollowed is both a semantics and a
// security guard: an earlier symlink "a" pointing at an ABSOLUTE host path must
// be replaced by a later regular-file entry "a", and the write must never reach
// the host path.
func TestExtractTar_AbsoluteSymlinkLeafReplacedNotFollowed(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "rootfs")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hostFile := filepath.Join(root, "host_secret")
	if err := os.WriteFile(hostFile, []byte("HOST-ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}

	data := buildTar(t, []tarEntry{
		{name: "a", typeflag: tar.TypeSymlink, linkname: hostFile, mode: 0o777},
		{name: "a", typeflag: tar.TypeReg, content: "A-CONTENT"},
	})

	_ = extractTar(t.Context(), bytes.NewReader(data), targetDir)

	if got, _ := os.ReadFile(hostFile); string(got) != "HOST-ORIGINAL" {
		t.Errorf("host file was written through the leaf symlink: content = %q", got)
	}
	aPath := filepath.Join(targetDir, "a")
	if isSymlink(t, aPath) {
		t.Errorf("%s should have been replaced by a regular file", aPath)
	}
	if got, _ := os.ReadFile(aPath); string(got) != "A-CONTENT" {
		t.Errorf("a content = %q, want %q", got, "A-CONTENT")
	}
}

// TestExtractTar_WhiteoutRemovesSymlinkNotReferent verifies that a whiteout
// ".wh.a" removes the symlink "a" itself, not the file the symlink points to.
// Resolving the whiteout path with SecureJoin follows the leaf symlink and
// deletes the referent instead.
func TestExtractTar_WhiteoutRemovesSymlinkNotReferent(t *testing.T) {
	targetDir := t.TempDir()

	data := buildTar(t, []tarEntry{
		{name: "b", typeflag: tar.TypeReg, content: "B"},
		{name: "a", typeflag: tar.TypeSymlink, linkname: "b", mode: 0o777},
		{name: ".wh.a", typeflag: tar.TypeReg}, // whiteout of "a"
	})

	if err := extractTar(t.Context(), bytes.NewReader(data), targetDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(targetDir, "a")); !os.IsNotExist(err) {
		t.Errorf("symlink a should have been removed by the whiteout")
	}
	if got, err := os.ReadFile(filepath.Join(targetDir, "b")); err != nil || string(got) != "B" {
		t.Errorf("referent b should be intact: content=%q err=%v", got, err)
	}
}
