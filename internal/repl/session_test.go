package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func goldenSession(t *testing.T, name string, steps func(f *fixture)) {
	t.Helper()
	f := newSession(t, nil)
	f.waitFor(prompt)
	steps(f)
	require.NoError(t, f.s.Close())
	got := f.out.String()
	path := filepath.Join("testdata", "session", name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create the golden")
	assert.Equal(t, string(want), got)
}

func TestSessionGolden(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		goldenSession(t, "basic", func(f *fixture) { f.run("select 1;\r") })
	})
	t.Run("multiline", func(t *testing.T) {
		goldenSession(t, "multiline", func(f *fixture) { f.typ("select 1,\r"); f.waitFor("· "); f.run("2;\r") })
	})
	t.Run("paste", func(t *testing.T) {
		goldenSession(t, "paste", func(f *fixture) {
			f.run("\x1b[200~select 1;\rselect 2;\r\x1b[201~")
			f.waitCount(prompt, 3)
		})
	})
	t.Run("dot_help", func(t *testing.T) {
		goldenSession(t, "dot_help", func(f *fixture) { f.run(".help\r") })
	})
	t.Run("dot_unknown", func(t *testing.T) {
		goldenSession(t, "dot_unknown", func(f *fixture) { f.run(".frobnicate\r") })
	})
	t.Run("dot_refused", func(t *testing.T) {
		goldenSession(t, "dot_refused", func(f *fixture) { f.run(".shell ls\r") })
	})
	t.Run("two_statements_one_line", func(t *testing.T) {
		goldenSession(t, "two_statements_one_line", func(f *fixture) { f.run("select 1; select 2;\r") })
	})
}

func TestBannerAndPrompt(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	assert.Contains(t, f.out.String(), "Vibe console on ws. Statements end with ';'. Enter .help for commands.")
	assert.True(t, strings.HasPrefix(f.out.String(), "\x1b[?2004h"), "bracketed paste is switched on first: %q", f.out.String())
}

func TestEscapesAreOnlyTheKnownOnes(t *testing.T) {
	f := newSession(t, func(o *Options) { o.Color = true })
	f.waitFor("ws ▸")
	f.typ("select chr(27) || '[31mred' || chr(155) || chr(7) as c;\r")
	f.waitFor("[31mred") // the hostile value, rendered inert
	require.NoError(t, f.s.Close())
	out := f.out.String()
	for _, known := range []string{sgrDim, sgrBold, sgrRed, sgrReset, "\x1b[?2004h", "\x1b[?2004l"} {
		out = strings.ReplaceAll(out, known, "")
	}
	assert.NotContains(t, out, "\x1b", "only the console's own sequences, and x/term's cursor motion, remain")
}
