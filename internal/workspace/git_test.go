package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHead(t *testing.T, dir, head string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HEAD"), []byte(head), 0o600))
}

func TestGitInfo(t *testing.T) {
	dir := t.TempDir()
	_, isRepo := GitInfo(dir)
	assert.False(t, isRepo)
	writeHead(t, filepath.Join(dir, ".git"), "ref: refs/heads/feature/x\n")
	br, isRepo := GitInfo(dir)
	assert.True(t, isRepo)
	assert.Equal(t, "feature/x", br)
}

func TestGitInfo_DetachedHeadIsTheShortCommit(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, filepath.Join(dir, ".git"), "0123456789abcdef0123456789abcdef01234567\n")
	br, isRepo := GitInfo(dir)
	assert.True(t, isRepo)
	assert.Equal(t, "0123456", br)
}

func TestGitInfo_WorktreeAndSubmoduleGitFile(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "gitdir")
	writeHead(t, real, "ref: refs/heads/wt\n")
	// An absolute gitdir.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+real+"\n"), 0o600))
	br, isRepo := GitInfo(root)
	assert.True(t, isRepo)
	assert.Equal(t, "wt", br)
	// A relative gitdir is relative to the workspace folder.
	writeHead(t, filepath.Join(root, "modules", "m"), "ref: refs/heads/rel\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: modules/m\n"), 0o600))
	br, isRepo = GitInfo(root)
	assert.True(t, isRepo)
	assert.Equal(t, "rel", br)
}

func TestGitInfo_UnreadableRepositoryStillCountsAsOne(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("not a gitdir pointer\n"), 0o600))
	br, isRepo := GitInfo(root)
	assert.True(t, isRepo)
	assert.Empty(t, br)
	// A .git directory with no HEAD.
	root2 := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root2, ".git"), 0o755))
	br, isRepo = GitInfo(root2)
	assert.True(t, isRepo)
	assert.Empty(t, br)
}

// GitInfo never runs git: a repository's own config (core.fsmonitor) names a
// program git would execute on status. Prove it with a PATH that has no git.
func TestGitInfo_RunsNoProcess(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := exec.LookPath("git")
	require.Error(t, err)
	dir := t.TempDir()
	writeHead(t, filepath.Join(dir, ".git"), "ref: refs/heads/main\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n\tfsmonitor = touch "+filepath.Join(dir, "PWNED")+"\n"), 0o600))
	br, _ := GitInfo(dir)
	assert.Equal(t, "main", br)
	assert.NoFileExists(t, filepath.Join(dir, "PWNED"))
	assert.False(t, GitAvailable())
}

func TestHasConfigFile(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, HasConfigFile(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte("id: x\n"), 0o600))
	assert.True(t, HasConfigFile(dir))
	// A symlink named vibe.yaml is not a workspace mark.
	other := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(dir, ConfigFile), filepath.Join(other, ConfigFile)))
	assert.False(t, HasConfigFile(other))
}

func TestExactRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte("id: x\n"), 0o600))
	root, ok := ExactRoot(dir)
	assert.True(t, ok)
	assert.NotEmpty(t, root)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	_, ok = ExactRoot(sub)
	assert.False(t, ok, "a folder inside a workspace is not the workspace")
	_, ok = ExactRoot(t.TempDir())
	assert.False(t, ok)
}
