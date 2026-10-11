package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	in := "\xef\xbb\xbf# a comment\r\n" +
		"MOTHERDUCK_TOKEN=md-test-not-a-real-token\r\n" +
		"export PGUSER=rosa\n" +
		"  SPACED  =  value with spaces  \n" +
		"DQ=\"a \\\"quoted\\\" #not comment\\n2\"  # trailing\n" +
		"SQ='literal $HOME \\n' # trailing\n" +
		"INLINE=abc # comment\n" +
		"HASHY=abc#def\n" +
		"EMPTY=\n" +
		"export\tTABBED=1\n" +
		"DUP=1\nDUP=2\n"
	p := Parse([]byte(in))
	assert.Empty(t, p.Warnings)
	assert.Equal(t, map[string]string{
		"MOTHERDUCK_TOKEN": "md-test-not-a-real-token",
		"PGUSER":           "rosa",
		"SPACED":           "value with spaces",
		"DQ":               "a \"quoted\" #not comment\n2",
		"SQ":               "literal $HOME \\n",
		"INLINE":           "abc",
		"HASHY":            "abc#def",
		"EMPTY":            "",
		"TABBED":           "1",
		"DUP":              "2",
	}, p.Vars)
	assert.True(t, sortedStrings(p.Names))
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestParseInvalidLinesWarnWithoutValues(t *testing.T) {
	in := "GOOD=1\nnot a pair\n9BAD=x\nBAD KEY=x\nOPEN=\"secret-value-open\nAFTER=\"x\" junk\n=novalue\nexportFOO=1\nOK2=2\n"
	p := Parse([]byte(in))
	assert.Equal(t, []string{"GOOD", "OK2", "exportFOO"}, p.Names)
	require.Len(t, p.Warnings, 5)
	for _, w := range p.Warnings {
		assert.NotContains(t, w, "secret-value-open")
		assert.NotContains(t, w, "junk")
	}
	assert.Contains(t, strings.Join(p.Warnings, "\n"), "line 5 (OPEN)")
}

func TestParseDoesNotExpandOrExecute(t *testing.T) {
	t.Setenv("HOME_X", "expanded")
	p := Parse([]byte("A=$HOME_X\nB=${HOME_X}\nC=$(echo pwned)\nD=`echo pwned`\n"))
	assert.Equal(t, "$HOME_X", p.Vars["A"])
	assert.Equal(t, "${HOME_X}", p.Vars["B"])
	assert.Equal(t, "$(echo pwned)", p.Vars["C"])
	assert.Equal(t, "`echo pwned`", p.Vars["D"])
}

func TestReadFile(t *testing.T) {
	root := t.TempDir()
	assert.False(t, Read(root).Present)
	assert.Empty(t, Read(root).Ignored)

	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("A=1\n"), 0o600))
	f := Read(root)
	assert.True(t, f.Present)
	assert.Len(t, f.Hash, 64)
	assert.Equal(t, filepath.Join(root, ".env"), f.Path)

	big := strings.Repeat("A=1\n", MaxSize/4+10)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte(big), 0o600))
	f = Read(root)
	assert.False(t, f.Present)
	assert.Contains(t, f.Ignored, "larger")
}

func TestReadSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere.env")
	require.NoError(t, os.WriteFile(outside, []byte("A=1\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".env")))
	f := Read(root)
	assert.False(t, f.Present)
	assert.Contains(t, f.Ignored, "outside the workspace")
	assert.Empty(t, f.Vars)

	// A link that stays inside the workspace is fine.
	require.NoError(t, os.Remove(filepath.Join(root, ".env")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.env"), []byte("A=1\n"), 0o600))
	require.NoError(t, os.Symlink("real.env", filepath.Join(root, ".env")))
	assert.True(t, Read(root).Present)
}

func TestDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dotenv.json")
	d := NewDecisions(path)
	p := "/ws/.env"
	assert.Equal(t, StateNone, d.State(p, ""))
	assert.Equal(t, StateUndecided, d.State(p, "h1"))

	require.NoError(t, d.Allow(p, "h1"))
	assert.Equal(t, StateAllowed, d.State(p, "h1"))
	assert.Equal(t, StateChanged, d.State(p, "h2"), "any content change ends an allow")
	assert.Equal(t, StateUndecided, d.State("/other/.env", "h1"), "keyed by path")

	// Persisted, and a new store sees it.
	assert.Equal(t, StateAllowed, NewDecisions(path).State(p, "h1"))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	raw, _ := os.ReadFile(path)
	assert.NotContains(t, string(raw), "token")

	require.NoError(t, d.Deny(p))
	assert.Equal(t, StateDenied, d.State(p, "h1"))
	assert.Equal(t, StateDenied, d.State(p, "h2"), "a deny holds across edits")

	require.NoError(t, d.Revoke(p))
	assert.Equal(t, StateUndecided, d.State(p, "h1"))
	require.NoError(t, d.Revoke(p), "revoking nothing is fine")
	assert.Error(t, d.Allow(p, ""))
}

func TestDecisionsMemoryAndCorruptFile(t *testing.T) {
	m := NewDecisions("")
	require.NoError(t, m.Allow("/a/.env", "h"))
	assert.Equal(t, StateAllowed, m.State("/a/.env", "h"))

	path := filepath.Join(t.TempDir(), "dotenv.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	d := NewDecisions(path)
	assert.Equal(t, StateUndecided, d.State("/a/.env", "h"), "a corrupt file allows nothing")
	require.NoError(t, d.Allow("/a/.env", "h"))
	assert.Equal(t, StateAllowed, d.State("/a/.env", "h"))
}

func TestReadFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, ".env"), 0o600))
	done := make(chan Loaded, 1)
	go func() { done <- Read(dir) }()
	select {
	case l := <-done:
		assert.False(t, l.Present)
		assert.Contains(t, l.Ignored, "not a regular file")
	case <-time.After(2 * time.Second):
		t.Fatal("Read hangs on a FIFO .env")
	}
}

func TestParseMultilineQuotedValueLeaksNothing(t *testing.T) {
	in := "A=1\nPEM=\"-----BEGIN KEY-----\nNOTAVAR=leaked\nsecretline\n-----END KEY-----\"\nB=2\n"
	p := Parse([]byte(in))
	assert.Equal(t, []string{"A", "B"}, p.Names)
	require.Len(t, p.Warnings, 1)
	assert.Contains(t, p.Warnings[0], "(PEM)")
	assert.NotContains(t, p.Warnings[0], "leaked")
	// Never closed: everything after is skipped, one warning.
	p = Parse([]byte("A=1\nPEM='x\nNOTAVAR=leaked\n"))
	assert.Equal(t, []string{"A"}, p.Names)
	assert.Len(t, p.Warnings, 1)
}
