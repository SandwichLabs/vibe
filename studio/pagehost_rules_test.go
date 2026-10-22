//go:build !windows

package main

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkRulesDir(t *testing.T, parent string, name string) string {
	t.Helper()
	d := filepath.Join(parent, name)
	require.NoError(t, os.MkdirAll(filepath.Join(d, "inner"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(d, "inner", "list"), []byte("x"), 0o600))
	return d
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestParseRulesDirName_OnlyTheExactPattern(t *testing.T) {
	pid, ok := parseRulesDirName("vibe-rules-4242")
	assert.True(t, ok)
	assert.Equal(t, 4242, pid)
	for _, bad := range []string{"vibe-rules-", "vibe-rules-0", "vibe-rules-007", "vibe-rules--5", "vibe-rules-+5", "vibe-rules-5x",
		"vibe-rules-5/..", "xvibe-rules-5", "vibe-rules-99999999999", "vibe-rules-5 ", "vibe-rules-"} {
		_, ok := parseRulesDirName(bad)
		assert.False(t, ok, bad)
	}
}

func TestSweepStaleRulesDirs_RemovesOnlyDeadHelpersOwnFolders(t *testing.T) {
	parent := t.TempDir()
	dead := mkRulesDir(t, parent, "vibe-rules-111")
	alive := mkRulesDir(t, parent, "vibe-rules-999")
	self := mkRulesDir(t, parent, "vibe-rules-5")
	oddName := mkRulesDir(t, parent, "vibe-rules-abc")
	otherDir := mkRulesDir(t, parent, "something-111")
	aFile := filepath.Join(parent, "vibe-rules-222")
	require.NoError(t, os.WriteFile(aFile, []byte("x"), 0o600))
	// A link named like a stale folder, pointing at a folder that must survive.
	victim := mkRulesDir(t, t.TempDir(), "precious")
	link := filepath.Join(parent, "vibe-rules-333")
	require.NoError(t, os.Symlink(victim, link))

	sweepStaleRulesDirs(parent, 5, func(pid int) bool { return pid == 999 })

	assert.False(t, exists(dead), "a dead helper's folder is removed, contents and all")
	for _, keep := range []string{alive, self, oddName, otherDir, aFile, link, filepath.Join(victim, "inner", "list")} {
		assert.True(t, exists(keep), "kept: %s", keep)
	}
}

func TestSweepStaleRulesDirs_AMissingDirectoryIsFine(t *testing.T) {
	sweepStaleRulesDirs(filepath.Join(t.TempDir(), "nope"), 1, func(int) bool { return false })
}

func TestRemoveRulesDir_BuildsThePathFromThePidAndFollowsNoLink(t *testing.T) {
	parent := t.TempDir()
	mine := mkRulesDir(t, parent, rulesDirName(321))
	other := mkRulesDir(t, parent, rulesDirName(322))
	removeRulesDir(parent, 321)
	assert.False(t, exists(mine))
	assert.True(t, exists(other), "only that helper's folder")
	removeRulesDir(parent, 0)
	removeRulesDir(parent, -4)
	assert.True(t, exists(other))

	victim := mkRulesDir(t, t.TempDir(), "precious")
	require.NoError(t, os.Symlink(victim, filepath.Join(parent, rulesDirName(400))))
	removeRulesDir(parent, 400)
	assert.True(t, exists(filepath.Join(victim, "inner", "list")), "a link is never followed")
}

func TestPidAlive(t *testing.T) {
	assert.True(t, pidAlive(os.Getpid()))
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	assert.False(t, pidAlive(cmd.Process.Pid), "a reaped process is gone")
}

func TestRulesParentFor(t *testing.T) {
	get := func(v string) func(string) string { return func(string) string { return v } }
	for in, want := range map[string]string{
		"/var/folders/x/T":   "/var/folders/x/T",
		"/var/folders/x/T/":  "/var/folders/x/T", // macOS $TMPDIR ends in a slash
		"/var/folders//x/T/": "/var/folders/x/T",
	} {
		got, ok := rulesParentFor(get(in))
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"relative/dir", "/a/../b", "/a/..", "../x", ""} {
		_, ok := rulesParentFor(get(bad))
		assert.False(t, ok, "no parent from %q: the helper uses its own temp dir and sweeps nothing", bad)
	}
}

func TestStudioRulesParent_FromTMPDIR(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/ab/cd/T/")
	got, ok := studioRulesParent()
	assert.True(t, ok)
	assert.Equal(t, "/var/folders/ab/cd/T", got)
	t.Setenv("TMPDIR", "")
	_, ok = studioRulesParent()
	assert.False(t, ok, "no TMPDIR: nothing is passed and nothing removed by path")
}

func TestStartHelper_RunsInRootNotStudiosDirectory(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	conn, err := startHelper(slog.Default(), func(in, out *os.File) (*exec.Cmd, func(), error) {
		cmd := exec.Command("/bin/sh", "-c", "pwd >&4")
		cmd.ExtraFiles = []*os.File{in, out}
		return cmd, func() {}, nil
	})
	require.NoError(t, err)
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	assert.Equal(t, "/\n", string(buf[:n]))
	<-conn.Done()
	pc, ok := conn.(*procConn)
	require.True(t, ok)
	assert.Equal(t, 0, pc.ExitCode())
}
