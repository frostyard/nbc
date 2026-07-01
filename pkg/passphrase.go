package pkg

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// readPassphrase prompts the user and reads a single-line secret.
//
// When stdin is a terminal, the input is read without echoing it, so the
// passphrase is not exposed in the terminal or its scrollback. When stdin is not
// a terminal (e.g. piped input in automation), a full line is read from stdin.
//
// Unlike fmt.Scanln, the ENTIRE line is returned, so passphrases containing
// spaces (e.g. a diceware phrase) are preserved rather than truncated at the
// first space.
func readPassphrase(prompt string) (string, error) {
	// Prompt on stderr so it never contaminates stdout (e.g. JSON output).
	fmt.Fprint(os.Stderr, prompt)

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		// ReadPassword consumes the newline but does not echo one; emit it so
		// subsequent output starts on a fresh line.
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	return readPassphraseLine(os.Stdin)
}

// readPassphraseLine reads one line from r and returns it with the trailing
// line terminator (\n or \r\n) removed. Interior and leading/trailing spaces are
// preserved. A final line with no newline (EOF) is returned as-is.
func readPassphraseLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
