package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"visual wins", map[string]string{"VISUAL": "nvim", "EDITOR": "nano"}, []string{"nvim"}},
		{"editor", map[string]string{"EDITOR": "hx"}, []string{"hx"}},
		{"arguments", map[string]string{"EDITOR": "code --wait"}, []string{"code", "--wait"}},
		{"quoted path", map[string]string{"EDITOR": `"/Applications/My Editor/bin/ed" -w`}, []string{"/Applications/My Editor/bin/ed", "-w"}},
		{"single quotes", map[string]string{"EDITOR": `emacsclient -a '' -t`}, []string{"emacsclient", "-a", "", "-t"}},
		{"fallback", map[string]string{"EDITOR": "  "}, []string{Fallback}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(env(tc.env))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := Resolve(env(map[string]string{"EDITOR": `vim "unclosed`}))
	assert.ErrorIs(t, err, ErrUnbalancedQuote)
}

func TestNewScratch_UnderWorkspace(t *testing.T) {
	root := t.TempDir()
	path, err := NewScratch(root, "select 1")
	require.NoError(t, err)
	assert.Equal(t, ScratchDir(root), filepath.Dir(path))
	assert.Equal(t, ".sql", filepath.Ext(path))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "select 1", string(b))
}

// fakeEditor is an argv that runs script with the file as $0.
func fakeEditor(t *testing.T, script string) []string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	return []string{"sh", "-c", script}
}

func TestEdit_ReturnsSavedText(t *testing.T) {
	root := t.TempDir()
	argv := fakeEditor(t, `test "$ASSAY_WORKSPACE" = "`+root+`" || exit 3; printf 'select 2' > "$0"`)
	text, ok, err := Edit(argv, root, "select 1", nil, nil, nil)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "select 2", text)
	left, _ := os.ReadDir(ScratchDir(root))
	assert.Empty(t, left, "the scratch file is removed")
}

func TestEdit_NonZeroExitCancels(t *testing.T) {
	root := t.TempDir()
	text, ok, err := Edit(fakeEditor(t, `printf 'drop table t' > "$0"; exit 1`), root, "", nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, ok, ":cq cancels, whatever was written")
	assert.Empty(t, text)
}

func TestEdit_MissingEditorIsAnError(t *testing.T) {
	_, _, err := Edit([]string{"/nonexistent/editor"}, t.TempDir(), "", nil, nil, nil)
	assert.Error(t, err)
}

func TestResolve_NoShellInvolved(t *testing.T) {
	got, err := Resolve(env(map[string]string{"EDITOR": `vim; touch pwned $(id)`}))
	require.NoError(t, err)
	assert.Equal(t, []string{"vim;", "touch", "pwned", "$(id)"}, got, "metacharacters stay literal words")
}

func TestCheck_MissingEditorNamesIt(t *testing.T) {
	err := Check([]string{"definitely-not-an-editor-xyz", "--wait"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-an-editor-xyz")
	assert.Contains(t, err.Error(), "$EDITOR")
	assert.Error(t, Check(nil))
}

func TestNewScratch_PrivateModes(t *testing.T) {
	root := t.TempDir()
	path, err := NewScratch(root, "select 1")
	require.NoError(t, err)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(ScratchDir(root))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}

func TestNewScratch_RefusesSymlinkedScratchDir(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".assay-local"), 0o700))
	require.NoError(t, os.Symlink(outside, ScratchDir(root)))
	_, err := NewScratch(root, "select 1")
	require.Error(t, err)
	left, _ := os.ReadDir(outside)
	assert.Empty(t, left, "nothing was created through the link")

	root = t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".assay-local")))
	_, err = NewScratch(root, "select 1")
	require.Error(t, err)
	left, _ = os.ReadDir(outside)
	assert.Empty(t, left)
}

func TestNewScratch_TightensALooseScratchDir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(ScratchDir(root), 0o755))
	require.NoError(t, os.Chmod(ScratchDir(root), 0o755))
	_, err := NewScratch(root, "select 1")
	require.NoError(t, err)
	di, err := os.Stat(ScratchDir(root))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}
