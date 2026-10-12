package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"golang.org/x/term"
)

// --- credential prompting ---

// lineReader hands out lines from one buffered reader, so several prompts can
// read a pipe without the first one swallowing the rest.
type lineReader struct {
	f  *os.File
	br *bufio.Reader
}

func newLineReader(in io.Reader) *lineReader {
	lr := &lineReader{}
	if f, ok := in.(*os.File); ok {
		lr.f = f
	}
	lr.br = bufio.NewReader(in)
	return lr
}

func (l *lineReader) tty() bool { return l.f != nil && term.IsTerminal(int(l.f.Fd())) }

func (l *lineReader) line(errW io.Writer, prompt string, secret bool) (string, error) {
	if l.tty() {
		fmt.Fprint(errW, prompt)
		if secret {
			b, err := term.ReadPassword(int(l.f.Fd()))
			fmt.Fprintln(errW)
			if err != nil {
				return "", fmt.Errorf("failed to read input: %w", err)
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	s, err := l.br.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || s == "") {
		return "", nil //nolint:nilerr // end of input: the remaining fields stay empty
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// promptCredentials asks for each credential field of the type. A blank answer
// keeps what is already stored (empty fields mean "keep" in SetCredentials).
func promptCredentials(in io.Reader, errW io.Writer, typ string, _ secrets.Credential) (secrets.Credential, error) {
	info, err := connections.TypeInfoFor(typ)
	if err != nil {
		return secrets.Credential{}, err
	}
	lr := newLineReader(in)
	var c secrets.Credential
	for _, f := range info.Credentials {
		label := f.Label
		if !f.Required {
			label += " (optional)"
		}
		v, err := lr.line(errW, label+": ", f.Secret)
		if err != nil {
			return secrets.Credential{}, err
		}
		if v != "" {
			cliRedactor.Add(secretOnly(f, v)...)
		}
		switch f.Name {
		case "user":
			c.User = v
		case "password":
			c.Password = v
		case "token":
			c.Token = v
		case "key_id":
			c.KeyID = v
		case "secret":
			c.Secret = v
		}
	}
	return c, nil
}

func secretOnly(f connections.Field, v string) []string {
	if f.Secret {
		return []string{v}
	}
	return nil
}
