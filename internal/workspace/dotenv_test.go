package workspace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

const (
	tokA = "md-test-not-a-real-token"
	tokB = "md-test-second-fake-token"
)

func writeEnv(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte(body), 0o600))
}

// envWorkspace is a workspace with an env:MOTHERDUCK connection named md.
func envWorkspace(t *testing.T, baseEnv map[string]string) (Workspace, string) {
	t.Helper()
	ws, _, cfg := memWorkspace(t, baseEnv)
	require.NoError(t, ws.AddConnection(ScopeShared, ConnectionDef{Name: "md", Type: "motherduck", Database: "d"}))
	require.NoError(t, ws.SetSignIn("md", "env:MOTHERDUCK"))
	return ws, cfg
}

func reopen(t *testing.T, ws Workspace, cfg string) Workspace {
	t.Helper()
	r := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()), secrets.WithEnv(func(string) string { return "" }))
	out, err := Open(ws.Root(), WithConfigDir(cfg), WithUserConfig(UserConfig{Version: ConfigVersion}), WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })
	return out
}

func mdToken(t *testing.T, ws Workspace) (string, error) {
	t.Helper()
	cred, _, err := ConnectionCredential(context.Background(), ws, "md", nil)
	return cred.Token, err
}

func TestDotEnv_AllowLoadsIntoTheOverlayOnly(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")

	st := ws.DotEnv()
	assert.Equal(t, "undecided", st.State)
	assert.False(t, st.Loaded)
	_, err := mdToken(t, ws)
	var need *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &need, "an undecided .env supplies nothing")
	assert.Contains(t, err.Error(), "allow loading it")

	require.NoError(t, ws.AllowDotEnv())
	st = ws.DotEnv()
	assert.Equal(t, "allowed", st.State)
	assert.True(t, st.Loaded)
	tok, err := mdToken(t, ws)
	require.NoError(t, err)
	assert.Equal(t, tokA, tok)

	// Never the process environment.
	_, set := os.LookupEnv("MOTHERDUCK_TOKEN")
	assert.False(t, set)
	for _, kv := range os.Environ() {
		assert.NotContains(t, kv, tokA)
	}
}

func TestDotEnv_StatusNamesOnlyAndSatisfies(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "export MOTHERDUCK_TOKEN="+tokA+"\nOTHER=zzz-fake\nbad line "+tokB+"\n")
	require.NoError(t, ws.AllowDotEnv())

	st := ws.DotEnv()
	assert.Equal(t, []string{"MOTHERDUCK_TOKEN", "OTHER"}, st.Names)
	require.Len(t, st.Satisfies, 1)
	assert.Equal(t, "md", st.Satisfies[0].Connection)
	assert.Equal(t, []string{"MOTHERDUCK_TOKEN"}, st.Satisfies[0].Variables)
	require.Len(t, st.Warnings, 1)

	raw, err := json.Marshal(st)
	require.NoError(t, err)
	for _, v := range []string{tokA, tokB, "zzz-fake"} {
		assert.NotContains(t, string(raw), v)
	}
}

func TestDotEnv_ChangeEndsTheAllowButLoadedValuesStayUntilReapproved(t *testing.T) {
	ws, cfg := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	require.NoError(t, ws.AllowDotEnv())

	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokB+"\n")
	st := ws.DotEnv()
	assert.Equal(t, "changed", st.State)
	assert.True(t, st.Loaded)
	tok, err := mdToken(t, ws)
	require.NoError(t, err)
	assert.Equal(t, tokA, tok, "the approved values stay loaded; the new content is not used")

	// A fresh open does not load a changed file.
	other := reopen(t, ws, cfg)
	assert.Equal(t, "changed", other.DotEnv().State)
	assert.False(t, other.DotEnv().Loaded)
	_, err = mdToken(t, other)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since you allowed")

	// Re-approving reloads without reopening.
	require.NoError(t, ws.AllowDotEnv())
	assert.Equal(t, "allowed", ws.DotEnv().State)
	tok, err = mdToken(t, ws)
	require.NoError(t, err)
	assert.Equal(t, tokB, tok)
	// The other handle on the same folder sees the decision on its next read.
	assert.Equal(t, "allowed", other.DotEnv().State)
}

func TestDotEnv_AllowSurvivesReopenWhenUnchanged(t *testing.T) {
	ws, cfg := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	require.NoError(t, ws.AllowDotEnv())

	again := reopen(t, ws, cfg)
	assert.True(t, again.DotEnv().Loaded)
	tok, err := mdToken(t, again)
	require.NoError(t, err)
	assert.Equal(t, tokA, tok)
}

func TestDotEnv_DenyHoldsAcrossEditsUntilRevoked(t *testing.T) {
	ws, cfg := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	require.NoError(t, ws.AllowDotEnv())
	require.NoError(t, ws.DenyDotEnv())
	assert.False(t, ws.DotEnv().Loaded, "deny drops loaded values")

	_, err := mdToken(t, ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied loading it")

	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokB+"\n")
	again := reopen(t, ws, cfg)
	assert.Equal(t, "denied", again.DotEnv().State, "edits do not re-nag a deny")

	require.NoError(t, again.RevokeDotEnv())
	assert.Equal(t, "undecided", again.DotEnv().State)
	assert.Equal(t, "undecided", reopen(t, ws, cfg).DotEnv().State)
}

func TestDotEnv_NoFileIsNone(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	assert.Equal(t, "none", ws.DotEnv().State)
	assert.Error(t, ws.AllowDotEnv())
	assert.Equal(t, []string{}, ws.DotEnv().Names)
}

func TestDotEnv_DoesNotLeakToAnotherWorkspace(t *testing.T) {
	a, _ := envWorkspace(t, nil)
	b, _ := envWorkspace(t, nil)
	writeEnv(t, a.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	writeEnv(t, b.Root(), "MOTHERDUCK_TOKEN="+tokB+"\n")
	require.NoError(t, a.AllowDotEnv())

	_, err := mdToken(t, b)
	require.Error(t, err, "b never allowed its .env, and a's values are not b's")
	tok, err := mdToken(t, a)
	require.NoError(t, err)
	assert.Equal(t, tokA, tok)

	// And with one shared resolver (Studio): each workspace still sees only its own.
	shared := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()), secrets.WithEnv(func(string) string { return "" }))
	open := func(src Workspace) Workspace {
		w, err := Open(src.Root(), WithConfigDir(t.TempDir()), WithUserConfig(UserConfig{Version: ConfigVersion}), WithResolver(shared))
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Close() })
		return w
	}
	wa, wb := open(a), open(b)
	require.NoError(t, wa.AllowDotEnv())
	for _, w := range []Workspace{wa, wb} {
		require.NoError(t, w.SetSignIn("md", "env:MOTHERDUCK"))
	}
	ta, err := mdToken(t, wa)
	require.NoError(t, err)
	assert.Equal(t, tokA, ta)
	_, err = mdToken(t, wb)
	require.Error(t, err)
	// The shared base resolver itself never sees the overlay.
	_, ok := shared.Resolve(context.Background(), secrets.WorkspaceRef("x", "md"), "env:MOTHERDUCK", secrets.Binding{Type: "motherduck"})
	require.Error(t, ok)
}

func TestDotEnv_OverlayWinsOverProcessEnvButFallsBack(t *testing.T) {
	ws, _ := envWorkspace(t, map[string]string{"MOTHERDUCK_TOKEN": "from-process", "MOTHERDUCK_USER": "u"})
	tok, err := mdToken(t, ws)
	require.NoError(t, err)
	assert.Equal(t, "from-process", tok)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	require.NoError(t, ws.AllowDotEnv())
	tok, err = mdToken(t, ws)
	require.NoError(t, err)
	assert.Equal(t, tokA, tok)
}

func TestDotEnv_ValuesAreRegisteredWithTheRedactor(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\nUNRELATED=some-other-fake-value\n")
	assert.Equal(t, tokA, ws.Resolver().Redactor().Redact(tokA), "nothing registered before allow")
	require.NoError(t, ws.AllowDotEnv())
	red := ws.Resolver().Redactor()
	assert.NotContains(t, red.Redact("failed: "+tokA+" rejected"), tokA)
	assert.NotContains(t, red.Redact("boom some-other-fake-value"), "some-other-fake-value", "every loaded value, used or not")
}

func TestDotEnv_DecisionsLiveInTheConfigDirNotTheFolder(t *testing.T) {
	ws, cfg := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	require.NoError(t, ws.AllowDotEnv())

	var found string
	require.NoError(t, filepath.WalkDir(cfg, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == DotEnvDecisionFile {
			found = p
		}
		return err
	}))
	require.NotEmpty(t, found)
	assert.Equal(t, filepath.Dir(ws.LocalConfigPath()), filepath.Dir(found), "next to local.yaml")
	raw, _ := os.ReadFile(found)
	assert.NotContains(t, string(raw), tokA)

	require.NoError(t, filepath.WalkDir(ws.Root(), func(p string, d os.DirEntry, err error) error {
		assert.NotEqual(t, DotEnvDecisionFile, d.Name())
		return err
	}))
}

func TestDotEnv_SymlinkOutsideTheWorkspaceIsIgnored(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.WriteFile(outside, []byte("MOTHERDUCK_TOKEN="+tokA+"\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws.Root(), ".env")))
	st := ws.DotEnv()
	assert.Equal(t, "none", st.State)
	assert.Contains(t, st.Ignored, "outside")
	assert.Error(t, ws.AllowDotEnv())
}

func TestDotEnv_GitTrackedIsFlagged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	run := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", ws.Root()}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	assert.False(t, ws.DotEnv().TrackedByGit, "not a repository")
	run("init", "-q")
	assert.False(t, ws.DotEnv().TrackedByGit, "untracked")
	run("add", "-f", ".env")
	assert.True(t, ws.DotEnv().TrackedByGit)
}

func TestDotEnv_WatcherReportsChanges(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	w := startWatcher(t, ws)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	c := next(t, w)
	assert.Equal(t, Change{Kind: KindDotEnv, Op: OpCreated, ID: ".env", Path: ".env"}, c)
	require.NoError(t, ws.AllowDotEnv())
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokB+"\n")
	c = next(t, w)
	assert.Equal(t, KindDotEnv, c.Kind)
	assert.Equal(t, OpChanged, c.Op)
	assert.Equal(t, "changed", ws.DotEnv().State)
}

func TestDotEnv_GitCheckNeverRunsRepoConfiguredCode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "MOTHERDUCK_TOKEN="+tokA+"\n")
	out, err := exec.Command("git", "-C", ws.Root(), "init", "-q").CombinedOutput()
	require.NoError(t, err, string(out))
	marker := filepath.Join(t.TempDir(), "PWNED")
	hook := filepath.Join(t.TempDir(), "fsm.sh")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755))
	f, err := os.OpenFile(filepath.Join(ws.Root(), ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("[core]\n\tfsmonitor = " + hook + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_ = ws.DotEnv()
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "git ran the repository's fsmonitor")
}

func TestDotEnv_GitCheckSkipsNonRepositories(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "A=1\n")
	assert.False(t, ws.DotEnv().TrackedByGit)
}

func TestDotEnv_RedactionScope(t *testing.T) {
	ws, _ := envWorkspace(t, nil)
	writeEnv(t, ws.Root(), "PGPASSWORD=pw1xx\nPASSWORD=pw2xx\nDB_PASS=pw3xx\nPG_PWD=pw4xx\nTOKEN=tk5xx\nSECRET=sc6xx\nDATABASE_URL=u7xxx\nPORT=5432\nMOTHERDUCK_TOKEN="+tokA+"\nLONG_SETTING=abcdefghijklmnopqrstuvwxyz\nREGION=us-east-1\n")
	require.NoError(t, ws.AllowDotEnv())
	red := ws.Resolver().Redactor()
	for _, v := range []string{"pw1xx", "pw2xx", "pw3xx", "pw4xx", "tk5xx", "sc6xx", "u7xxx"} {
		assert.NotContains(t, red.Redact("err "+v+" end"), v)
	}
	assert.Equal(t, "port 5432 in us-east-1", red.Redact("port 5432 in us-east-1"))
	assert.NotContains(t, red.Redact("bad "+tokA), tokA)
	assert.NotContains(t, red.Redact("x abcdefghijklmnopqrstuvwxyz"), "abcdefghijklmnopqrstuvwxyz")
}

func TestDotEnv_GitCheckFindsTheEnclosingRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	res, err := Init(filepath.Join(repo, "sub", "acme"))
	require.NoError(t, err)
	_ = res.Workspace.Close()
	ws, err := Open(res.Workspace.Root(), noUser(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	writeEnv(t, ws.Root(), "A=1\n")
	assert.False(t, ws.DotEnv().TrackedByGit)
	run("add", "-f", "sub/acme/.env")
	assert.True(t, ws.DotEnv().TrackedByGit)

	marker := filepath.Join(t.TempDir(), "PWNED")
	hook := filepath.Join(t.TempDir(), "fsm.sh")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755))
	run("config", "core.fsmonitor", hook)
	writeEnv(t, ws.Root(), "A=2\n") // new mtime busts the cache
	_ = ws.DotEnv()
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr))
}
