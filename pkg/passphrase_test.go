package pkg

import (
	"strings"
	"testing"
)

// TestReadPassphraseLine_PreservesSpaces is the regression test for #91: the
// previous fmt.Scanln read stops at the first whitespace, silently truncating a
// passphrase that contains spaces (e.g. a diceware phrase). readPassphraseLine
// must return the entire line.
func TestReadPassphraseLine_PreservesSpaces(t *testing.T) {
	got, err := readPassphraseLine(strings.NewReader("correct horse battery staple\n"))
	if err != nil {
		t.Fatalf("readPassphraseLine error: %v", err)
	}
	if want := "correct horse battery staple"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadPassphraseLine_StripsTrailingNewlineAndCR(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"secret\n", "secret"},
		{"secret\r\n", "secret"},
		{"secret", "secret"}, // no trailing newline (EOF)
		{"with  double  spaces\n", "with  double  spaces"},
	} {
		got, err := readPassphraseLine(strings.NewReader(tc.in))
		if err != nil {
			t.Fatalf("input %q: unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("input %q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestReadPassphraseLine_DoesNotStripInteriorOrLeadingSpaces ensures we only
// trim the line terminator, not meaningful passphrase characters.
func TestReadPassphraseLine_DoesNotStripInteriorOrLeadingSpaces(t *testing.T) {
	got, err := readPassphraseLine(strings.NewReader("  leading and trailing kept \n"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if want := "  leading and trailing kept "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
