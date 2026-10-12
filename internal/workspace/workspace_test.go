package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// noUser isolates a workspace from the host: no user-scope connections, and a
// private config dir for the per-person settings.
func noUser(t *testing.T) Option {
	t.Helper()
	dir := t.TempDir()
	return func(o *options) {
		WithUserConfig(UserConfig{Version: ConfigVersion})(o)
		WithConfigDir(dir)(o)
	}
}

func TestMain(m *testing.M) {
	// Init opens the workspace with the default config dir; keep every test
	// away from the developer's real one.
	dir, err := os.MkdirTemp("", "vibe-ws-test-config-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("VIBE_CONFIG_DIR", dir)
	// Never touch the real keychain item set.
	_ = os.Setenv("VIBE_KEYRING_SERVICE", "vibe-test-workspace-pkg")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func newWorkspace(t *testing.T, opts ...InitOption) (Workspace, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "acme")
	res, err := Init(dir, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Workspace.Close() })
	// Re-open with an isolated user scope so the host's config never leaks in.
	ws, err := Open(res.Workspace.Root(), noUser(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws, ws.Root()
}

func TestInit_CreatesTheLayout(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	dir := filepath.Join(t.TempDir(), "My Workspace")
	res, err := Init(dir, WithName("Acme Analytics"))
	require.NoError(t, err)
	ws := res.Workspace
	defer ws.Close()

	for _, d := range []string{"notebooks", "queries", "skills", "data"} {
		assert.DirExists(t, filepath.Join(ws.Root(), d))
	}
	assert.FileExists(t, filepath.Join(ws.Root(), "vibe.yaml"))
	assert.FileExists(t, filepath.Join(ws.Root(), ".vibe-local", "nonce"), "the first open makes the folder's mark")

	assert.Equal(t, "Acme Analytics", ws.Name())
	_, err = uuidParse(ws.ID())
	assert.NoError(t, err)
	assert.Equal(t, "local", ws.DefaultConnection())
	conns := ws.Connections()
	require.Len(t, conns, 1)
	assert.Equal(t, "local", conns[0].Name)
	assert.Equal(t, "duckdb", conns[0].Type)
	assert.Equal(t, "data/local.duckdb", conns[0].Path)
	assert.False(t, conns[0].IsReadOnly())
	assert.Equal(t, ScopeShared, conns[0].Scope)
	assert.True(t, conns[0].Default)

	ig, _ := os.ReadFile(filepath.Join(ws.Root(), ".gitignore"))
	for _, l := range []string{".vibe-local/", "data/*.duckdb", "data/*.duckdb.wal"} {
		assert.Contains(t, string(ig), l+"\n")
	}
	ga, _ := os.ReadFile(filepath.Join(ws.Root(), ".gitattributes"))
	assert.Contains(t, string(ga), "*.vnb text eol=lf\n")

	// The file it wrote is something Open accepts as is.
	again, err := Open(ws.Root(), noUser(t))
	require.NoError(t, err)
	assert.Equal(t, ws.ID(), again.ID())
}

func TestInit_FreshIDEachTime(t *testing.T) {
	a, _ := newWorkspace(t)
	b, _ := newWorkspace(t)
	assert.NotEqual(t, a.ID(), b.ID())
}

func TestInit_MergesExistingIgnoreFilesWithoutClobbering(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n*.log"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("* text=auto\n*.vnb text eol=lf\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("keep me"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notebooks", "old.txt"), []byte("x"), 0o644))

	res, err := Init(dir)
	require.NoError(t, err)
	defer res.Workspace.Close()

	ig, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	assert.True(t, strings.HasPrefix(string(ig), "node_modules/\n*.log\n"), "existing lines stay first, newline repaired: %q", ig)
	assert.Contains(t, string(ig), ".vibe-local/\n")
	assert.Equal(t, 1, strings.Count(string(ig), "data/*.duckdb\n"))

	ga, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	assert.Equal(t, "* text=auto\n*.vnb text eol=lf\n", string(ga), "already satisfied: untouched")

	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	assert.Equal(t, "keep me", string(readme))
	assert.FileExists(t, filepath.Join(dir, "notebooks", "old.txt"))
}

func TestInit_RefusesExistingWorkspaceAndNesting(t *testing.T) {
	ws, root := newWorkspace(t)
	_, err := Init(root)
	assert.ErrorIs(t, err, ErrAlreadyWorkspace)

	_, err = Init(filepath.Join(root, "notebooks"))
	assert.ErrorIs(t, err, ErrAlreadyWorkspace, "inside an existing workspace")

	_, err = Init(filepath.Join(root, "brand", "new"))
	assert.ErrorIs(t, err, ErrAlreadyWorkspace)
	assert.NoDirExists(t, filepath.Join(root, "brand"), "nothing is created on refusal")

	// An existing vibe.yaml is never overwritten.
	before, _ := os.ReadFile(filepath.Join(root, "vibe.yaml"))
	_, err = Init(root, WithName("Other"))
	require.Error(t, err)
	after, _ := os.ReadFile(filepath.Join(root, "vibe.yaml"))
	assert.Equal(t, before, after)
	_ = ws
}

func TestInit_ValidatesBeforeWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	_, err := Init(dir, WithID("not-a-uuid"))
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "vibe.yaml"))
	assert.NoDirExists(t, filepath.Join(dir, "notebooks"), "no half-made workspace")
}

func TestInit_Git(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := filepath.Join(t.TempDir(), "g")
	res, err := Init(dir, WithGit(true))
	require.NoError(t, err)
	defer res.Workspace.Close()
	assert.True(t, res.GitInitialized)
	assert.DirExists(t, filepath.Join(dir, ".git"))

	// Already a repository: no second init, a note instead.
	dir2 := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "--quiet", dir2).Run())
	res2, err := Init(dir2, WithGit(true))
	require.NoError(t, err)
	defer res2.Workspace.Close()
	assert.False(t, res2.GitInitialized)
	assert.NotEmpty(t, res2.Notes)

	// Not requested: no repository.
	dir3 := t.TempDir()
	res3, err := Init(dir3)
	require.NoError(t, err)
	defer res3.Workspace.Close()
	assert.NoDirExists(t, filepath.Join(dir3, ".git"))
}

func TestInit_GitMissingFromPathIsANoteNotAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	res, err := Init(t.TempDir(), WithGit(true))
	require.NoError(t, err)
	defer res.Workspace.Close()
	assert.False(t, res.GitInitialized)
	require.NotEmpty(t, res.Notes)
	assert.Contains(t, res.Notes[0], "git")
}

func TestDiscover(t *testing.T) {
	_, root := newWorkspace(t)
	deep := filepath.Join(root, "notebooks", "a", "b")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	got, err := Discover(deep)
	require.NoError(t, err)
	assert.Equal(t, root, got)

	got, err = Discover(root)
	require.NoError(t, err)
	assert.Equal(t, root, got)

	// A file path and a not-yet-created path both walk up from their folder.
	got, err = Discover(filepath.Join(root, "vibe.yaml"))
	require.NoError(t, err)
	assert.Equal(t, root, got)
	got, err = Discover(filepath.Join(root, "does", "not", "exist"))
	require.NoError(t, err)
	assert.Equal(t, root, got)

	_, err = Discover(t.TempDir())
	assert.ErrorIs(t, err, ErrNotWorkspace)

	// The nearest vibe.yaml wins in a nested workspace.
	inner := filepath.Join(root, "inner")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inner, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	got, err = Discover(filepath.Join(inner, "x"))
	require.NoError(t, err)
	assert.Equal(t, inner, got)

	// Through a symlink to the same folder.
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(root, link))
	got, err = Discover(link)
	require.NoError(t, err)
	assert.Equal(t, root, got)

	// A directory called vibe.yaml is not a marker.
	trap := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(trap, "vibe.yaml"), 0o755))
	_, err = Discover(trap)
	assert.ErrorIs(t, err, ErrNotWorkspace)
}

func TestOpen_Errors(t *testing.T) {
	_, err := Open(t.TempDir(), noUser(t))
	assert.ErrorIs(t, err, ErrNotWorkspace)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\nmystery: 1\n"), 0o644))
	_, err = Open(dir, noUser(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mystery")

	dir = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("name: x\n"), 0o644))
	_, err = Open(dir, noUser(t))
	assert.ErrorIs(t, err, ErrMissingID)
	id, err := EnsureID(dir)
	require.NoError(t, err)
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	assert.Equal(t, id, ws.ID())
	again, err := EnsureID(dir)
	require.NoError(t, err)
	assert.Equal(t, id, again, "EnsureID is idempotent")
}

func TestOpen_SymlinkedVibeYamlIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "vibe.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("id: "+testID+"\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "vibe.yaml")))
	_, err := Open(dir, noUser(t))
	assert.Error(t, err)
}

func TestOpenFrom(t *testing.T) {
	_, root := newWorkspace(t)
	ws, err := OpenFrom(filepath.Join(root, "data"), noUser(t))
	require.NoError(t, err)
	assert.Equal(t, root, ws.Root())
}

func TestScopesMergeMostSpecificWins(t *testing.T) {
	user := UserConfig{Version: 1,
		Connections: []ConnectionDef{
			{Name: "warehouse", Type: "postgres", Host: "user-host"},
			{Name: "mine", Type: "sqlite", Path: "/home/me/x.db"},
		},
		SignIn: map[string]string{"mine": "env:MINE", "warehouse": "prompt"},
	}
	shared := Config{Version: 1, ID: testID, DefaultConnection: "warehouse", Connections: []ConnectionDef{
		{Name: "warehouse", Type: "postgres", Host: "shared-host", Credentials: "keychain"},
		{Name: "lake", Type: "s3", Scope: "s3://b/"},
	}}
	local := LocalConfig{Version: 1,
		Connections: []ConnectionDef{
			{Name: "warehouse", Type: "postgres", Host: "local-host"},
			{Name: "scratch", Type: "duckdb", Path: "/tmp/s.duckdb"},
		},
		SignIn: map[string]string{"lake": "env:LAKE"},
	}
	merged := Merge(user, shared, local, "warehouse")
	byName := map[string]Connection{}
	var names []string
	for _, c := range merged {
		byName[c.Name] = c
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"lake", "mine", "scratch", "warehouse"}, names, "sorted, one per name")

	w := byName["warehouse"]
	assert.Equal(t, ScopeLocal, w.Scope, "local beats shared beats user")
	assert.Equal(t, "local-host", w.Host, "the winner replaces the whole entry")
	assert.Equal(t, []Scope{ScopeShared, ScopeUser}, w.Shadows, "shadowing, most specific first")
	assert.True(t, w.Default)
	assert.Equal(t, "keychain", w.SignIn, "user-scope sign_in does not apply to a connection it did not win; no definition suggestion, so keychain")

	assert.Equal(t, ScopeUser, byName["mine"].Scope)
	assert.Equal(t, "env:MINE", byName["mine"].SignIn, "user connection uses the user-scope sign-in")
	assert.Equal(t, ScopeShared, byName["lake"].Scope)
	assert.Equal(t, "env:LAKE", byName["lake"].SignIn, "per-person sign-in lives in .vibe-local, whatever the scope")
	assert.Empty(t, byName["lake"].Shadows)
	assert.Equal(t, ScopeLocal, byName["scratch"].Scope)
	assert.NotEqual(t, byName["warehouse"].Fingerprint, Fingerprint(shared.Connections[0]), "fingerprint follows the winning definition")

	// A user-scope entry never overrides.
	only := Merge(user, Config{Connections: shared.Connections}, LocalConfig{}, "")
	for _, c := range only {
		if c.Name == "warehouse" {
			assert.Equal(t, ScopeShared, c.Scope)
			assert.Equal(t, []Scope{ScopeUser}, c.Shadows)
			assert.Equal(t, "keychain", c.SignIn, "definition's suggestion, not the user-scope setting")
		}
	}
}

func TestSignInDefaultsAndLocalOverride(t *testing.T) {
	shared := Config{Connections: []ConnectionDef{
		{Name: "a", Type: "postgres", Host: "h", Credentials: "env:PG_A"},
		{Name: "b", Type: "postgres", Host: "h"},
		{Name: "c", Type: "postgres", Host: "h", Credentials: "keychain"},
	}}
	merged := Merge(UserConfig{}, shared, LocalConfig{SignIn: map[string]string{"c": "prompt"}}, "")
	got := map[string]string{}
	for _, c := range merged {
		got[c.Name] = c.SignIn
	}
	assert.Equal(t, map[string]string{"a": "keychain", "b": "keychain", "c": "prompt"}, got, "a shared credentials: value is only a hint")
	for _, c := range merged {
		if c.Name == "a" {
			assert.Equal(t, "env:PG_A", c.SignInHint)
		}
	}
}

func TestOpenReadsAllThreeScopesAndReloads(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", cfgDir)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "connections.yaml"), []byte("connections:\n  - {name: personal, type: sqlite, path: /tmp/p.db}\n"), 0o644))

	dir := filepath.Join(t.TempDir(), "w")
	res, err := Init(dir)
	require.NoError(t, err)
	ws := res.Workspace
	defer ws.Close()
	writeLocal(t, ws, "connections:\n  - {name: scratch, type: duckdb, path: data/scratch.duckdb}\nsign_in:\n  scratch: prompt\nagent:\n  sandbox: read-only\n")

	require.NoError(t, ws.Reload())
	names := map[string]Scope{}
	for _, c := range ws.Connections() {
		names[c.Name] = c.Scope
	}
	assert.Equal(t, map[string]Scope{"local": ScopeShared, "personal": ScopeUser, "scratch": ScopeLocal}, names)
	assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox)

	// Edit vibe.yaml from outside (an editor, git pull) and reload.
	data, _ := os.ReadFile(filepath.Join(ws.Root(), "vibe.yaml"))
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "vibe.yaml"), []byte(strings.Replace(string(data), "name: "+filepath.Base(ws.Root()), "name: Renamed", 1)), 0o644))
	require.NoError(t, ws.Reload())
	assert.Equal(t, "Renamed", ws.Name())

	// A bad edit leaves the previous configuration in force.
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "vibe.yaml"), []byte("id: nope\n"), 0o644))
	require.Error(t, ws.Reload())
	assert.Equal(t, "Renamed", ws.Name())
}

func TestBrokenUserFileIsIgnoredWithWarning(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", cfgDir)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "connections.yaml"), []byte("connections: [oops"), 0o644))
	res, err := Init(filepath.Join(t.TempDir(), "w"))
	require.NoError(t, err)
	defer res.Workspace.Close()
	assert.Len(t, res.Workspace.Connections(), 1)
	require.NotEmpty(t, res.Workspace.Warnings())
	assert.Contains(t, res.Workspace.Warnings()[0], "ignored")
}

func TestUnknownDefaultConnectionWarns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\ndefault_connection: prod_pg\n"), 0o644))
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	require.Len(t, ws.Warnings(), 1)
	assert.Contains(t, ws.Warnings()[0], "prod_pg")
}

func TestSaveConfigAndLocalAreCompareAndWrite(t *testing.T) {
	ws, root := newWorkspace(t)
	cfg := ws.Config()
	cfg.Name = "Saved"
	require.NoError(t, ws.SaveConfig(cfg))
	disk, _ := os.ReadFile(filepath.Join(root, "vibe.yaml"))
	assert.Contains(t, string(disk), "name: Saved")

	// An outside edit makes the next save a conflict, not a clobber.
	require.NoError(t, os.WriteFile(filepath.Join(root, "vibe.yaml"), []byte(strings.Replace(string(disk), "name: Saved", "name: Outside", 1)), 0o644))
	cfg.Name = "Mine"
	err := ws.SaveConfig(cfg)
	assert.ErrorIs(t, err, ErrConflict)
	disk, _ = os.ReadFile(filepath.Join(root, "vibe.yaml"))
	assert.Contains(t, string(disk), "name: Outside")
	require.NoError(t, ws.Reload())
	cfg = ws.Config()
	cfg.Name = "Mine"
	require.NoError(t, ws.SaveConfig(cfg))

	// Invalid config is rejected before touching disk.
	bad := ws.Config()
	bad.Sandbox = "root"
	assert.Error(t, ws.SaveConfig(bad))

	l := ws.Local()
	l.Agent.Sandbox = SandboxReadOnly
	l.SignIn = map[string]string{"local": "prompt"}
	require.NoError(t, ws.SaveLocal(l))
	assert.FileExists(t, ws.LocalConfigPath())
	assert.NotContains(t, ws.LocalConfigPath(), root, "per-person settings are never written inside the folder")
	assert.NoDirExists(t, filepath.Join(root, ".vibe-local", "connections.yaml"))
	assert.NoFileExists(t, filepath.Join(root, ".vibe-local", "connections.yaml"))
	assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox)
	assert.Equal(t, "prompt", ws.Connections()[0].SignIn)
	l.SignIn = map[string]string{"x": "plaintext:hunter2"}
	assert.Error(t, ws.SaveLocal(l))
}

func TestStateRoundTripAndGarbage(t *testing.T) {
	ws, root := newWorkspace(t)
	assert.Equal(t, State{}, ws.State())
	require.NoError(t, ws.SaveState(State{LastNotebook: "weekly", OpenTabs: []string{"a", "b"}}))
	assert.Equal(t, State{LastNotebook: "weekly", OpenTabs: []string{"a", "b"}}, ws.State())
	require.NoError(t, os.WriteFile(filepath.Join(root, ".vibe-local", "state.json"), []byte("{garbage"), 0o644))
	assert.Equal(t, State{}, ws.State(), "unreadable state is empty, never an error")
}

func TestAgentPolicyThroughWorkspace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\nsandbox: read-write\nask_before_writes: true\n"), 0o644))
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	// You loosen everything in your OWN settings: still clamped to the ceiling.
	writeLocal(t, ws, "agent:\n  sandbox: full\n  ask_before_writes: false\n")
	sandbox, ask := ws.AgentPolicy()
	assert.Equal(t, SandboxReadWrite, sandbox, "clamped to the committed ceiling")
	assert.True(t, ask, "the committed gate cannot be loosened locally")
}

// Review #109 blocker 1 (D-A): a repository that commits .vibe-local/
// connections.yaml must not loosen the agent, redefine connections, redirect
// sign-in or add connections. That file is never read.
func TestCommittedVibeLocalIsIgnored(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "planted.duckdb")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+`
sandbox: read-only
ask_before_writes: true
default_connection: warehouse
connections:
  - {name: warehouse, type: postgres, host: db.internal, database: sales, credentials: keychain}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".vibe-local"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".vibe-local", "connections.yaml"), []byte(`
agent: {sandbox: full, ask_before_writes: false}
sign_in: {warehouse: "env:ATTACKER"}
connections:
  - {name: warehouse, type: postgres, host: evil.example, database: sales}
  - {name: planted, type: duckdb, path: '`+victim+`', read_only: false}
`), 0o644))

	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)

	sandbox, ask := ws.AgentPolicy()
	assert.Equal(t, SandboxReadOnly, sandbox, "policy stays at the committed ceiling")
	assert.True(t, ask)
	assert.Equal(t, SandboxReadOnly, string(agents.PolicyFrom(ws).Mode))

	conns := ws.Connections()
	require.Len(t, conns, 1, "no connection from the committed local file")
	assert.Equal(t, "db.internal", conns[0].Host, "the committed definition is not overridden")
	assert.Equal(t, ScopeShared, conns[0].Scope)
	assert.Equal(t, "keychain", conns[0].SignIn, "sign-in cannot be redirected")
	require.NotEmpty(t, ws.Warnings())
	assert.Contains(t, strings.Join(ws.Warnings(), " "), ".vibe-local/connections.yaml was ignored")

	sess, _, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	assert.NoFileExists(t, victim)

	// Even a hostile file that is not valid YAML at all cannot stop the open.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".vibe-local", "connections.yaml"), []byte("{{{ not yaml"), 0o644))
	ws2, err := Open(dir, noUser(t))
	require.NoError(t, err)
	assert.NotEmpty(t, ws2.Warnings())
}

func TestPerPersonSettingsAreKeyedByIDAndRootSoClonesAreIndependent(t *testing.T) {
	cfg := t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		require.NoError(t, os.WriteFile(filepath.Join(d, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	}
	wa, err := Open(a, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	wb, err := Open(b, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	assert.Equal(t, wa.ID(), wb.ID())
	assert.NotEqual(t, wa.LocalConfigPath(), wb.LocalConfigPath(), "same id, different clones: separate settings")
	assert.True(t, strings.HasPrefix(wa.LocalConfigPath(), filepath.Join(cfg, "workspaces", testID+"-")))
	assert.Equal(t, "local.yaml", filepath.Base(wa.LocalConfigPath()))

	require.NoError(t, wa.SaveLocal(LocalConfig{Agent: LocalAgent{Sandbox: SandboxReadOnly}}))
	assert.Equal(t, SandboxReadOnly, wa.Policy().Sandbox)
	assert.Equal(t, DefaultSandbox, wb.Policy().Sandbox, "the other clone does not see it")
	wb2, err := Open(b, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	assert.Equal(t, DefaultSandbox, wb2.Policy().Sandbox)
	wa2, err := Open(a, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	assert.Equal(t, SandboxReadOnly, wa2.Policy().Sandbox, "and it persists for this one")
}

func TestOpenSweepsStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	for _, d := range []string{"notebooks", "skills/foo", "queries"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0o755))
	}
	old := time.Now().Add(-time.Hour)
	var stale, fresh []string
	for _, d := range []string{"notebooks", "skills/foo", "queries"} {
		p := filepath.Join(dir, d, ".vibe-tmp-x.old")
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
		require.NoError(t, os.Chtimes(p, old, old))
		stale = append(stale, p)
		q := filepath.Join(dir, d, ".vibe-tmp-x.new")
		require.NoError(t, os.WriteFile(q, []byte("x"), 0o644))
		fresh = append(fresh, q)
	}
	_, err := Open(dir, noUser(t))
	require.NoError(t, err)
	for _, p := range stale {
		assert.NoFileExists(t, p)
	}
	for _, p := range fresh {
		assert.FileExists(t, p, "a recent temp file may belong to a write in flight")
	}
}

func TestWorkspaceStoresAreFileBackedAndSurfaceConflicts(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()

	nb, err := ws.Notebooks().Create(ctx, "Weekly revenue")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(root, "notebooks", "weekly-revenue.vnb"))

	q := &queries.Query{Title: "Top customers", SQL: "select 1", Connection: "local"}
	require.NoError(t, ws.Queries().Create(ctx, q))
	assert.FileExists(t, filepath.Join(root, "queries", "top-customers.sql"))

	// Edited from outside (editor, git pull), then loaded again.
	path := filepath.Join(root, "notebooks", "weekly-revenue.vnb")
	disk, _ := os.ReadFile(path)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(disk), "Weekly revenue", "Edited outside", 1)), 0o644))
	got, _, err := ws.Notebooks().Get(ctx, nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "Edited outside", got.Title)

	// Two writers: a stale save is a surfaced conflict, not a silent clobber.
	stale := got
	got.Title = "Writer one"
	_, _, err = ws.Notebooks().Save(ctx, got)
	require.NoError(t, err)
	stale.Title = "Writer two"
	_, _, err = ws.Notebooks().Save(ctx, stale)
	assert.ErrorIs(t, err, notebooks.ErrConflict)
}

func TestSessionOpensLocalDuckDBAsDefault(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	sess, issues, err := OpenSession(ctx, ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	assert.Empty(t, issues)
	assert.Equal(t, "local", sess.DefaultCatalog())
	assert.FileExists(t, filepath.Join(root, "data", "local.duckdb"))

	_, err = sess.Exec(ctx, "CREATE TABLE sales (n INT)")
	require.NoError(t, err)
	_, err = sess.Exec(ctx, "INSERT INTO sales VALUES (1), (2)")
	require.NoError(t, err)
	rows, err := sess.Query(ctx, "SELECT count(*) FROM sales")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var n int
	require.NoError(t, rows.Scan(&n))
	require.NoError(t, rows.Close())
	assert.Equal(t, 2, n)

	var seen bool
	for _, c := range sess.Connections() {
		if c.Name == "local" {
			seen = true
			assert.Equal(t, session.StatusOK, c.Kind)
			assert.True(t, c.Default)
			assert.False(t, c.ReadOnly)
		}
	}
	assert.True(t, seen)
}

func writeVibe(t *testing.T, dir, conns string) Workspace {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"+conns), 0o644))
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	return ws
}

// writeLocal writes the person's own settings file for ws and reloads it.
func writeLocal(t *testing.T, ws Workspace, yamlText string) {
	t.Helper()
	p := ws.LocalConfigPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	require.NoError(t, os.WriteFile(p, []byte(yamlText), 0o600))
	require.NoError(t, ws.Reload())
}

func TestSessionPathsCannotEscapeTheWorkspace(t *testing.T) {
	outside := t.TempDir()
	dir := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "linked")))
	ws := writeVibe(t, dir, `connections:
  - {name: viasym, type: duckdb, path: linked/x.duckdb, read_only: false}
  - {name: fine, type: duckdb, path: data/fine.duckdb, read_only: false}
`)
	// Relative paths in your own scope are confined to the folder too.
	writeLocal(t, ws, `connections:
  - {name: dotdot, type: duckdb, path: ../escape.duckdb, read_only: false}
  - {name: deep, type: duckdb, path: a/../../escape2.duckdb, read_only: false}
`)
	sess, issues, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	got := map[string]string{}
	for _, i := range issues {
		got[i.Connection] = i.Message
	}
	assert.Contains(t, got["dotdot"], "leaves the workspace")
	assert.Contains(t, got["viasym"], "symbolic link")
	assert.Contains(t, got["deep"], "leaves the workspace")
	assert.NotContains(t, got, "fine")
	entries, _ := os.ReadDir(outside)
	assert.Empty(t, entries, "nothing created outside")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dir), "escape.duckdb"))
	assert.FileExists(t, filepath.Join(dir, "data", "fine.duckdb"))
}

// Review #109 D-C: a committed vibe.yaml cannot make Vibe attach or create a
// file anywhere outside its own folder.
func TestSharedScopeCannotUseAbsolutePathsOrEnv(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim", "deep", "planted.duckdb")
	for name, conns := range map[string]string{
		"absolute": "connections:\n  - {name: x, type: duckdb, path: '" + victim + "', read_only: false}\n",
		"env":      "connections:\n  - {name: x, type: duckdb, path: '${PROBEHOME}/evil.duckdb', read_only: false}\n",
		"env host": "connections:\n  - {name: x, type: postgres, host: '${AWS_SECRET_ACCESS_KEY}.evil.example'}\n",
		"dotdot":   "connections:\n  - {name: x, type: duckdb, path: ../evil.duckdb, read_only: false}\n",
		"home":     "connections:\n  - {name: x, type: duckdb, path: '~/evil.duckdb', read_only: false}\n",
		"opt":      "connections:\n  - {name: x, type: postgres, host: h, options: {application_name: '${USER}'}}\n",
	} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"+conns), 0o644))
		_, err := Open(dir, noUser(t))
		assert.Error(t, err, name)
	}
	assert.NoDirExists(t, filepath.Dir(filepath.Dir(victim)))
}

func TestOwnScopeMayUseAbsolutePathsButNeverCreatesFoldersOutside(t *testing.T) {
	ws := writeVibe(t, t.TempDir(), "")
	base := t.TempDir()
	existing := filepath.Join(base, "big.duckdb")
	missingDir := filepath.Join(base, "not", "there", "x.duckdb")
	writeLocal(t, ws, "connections:\n  - {name: big, type: duckdb, path: '"+existing+"', read_only: false}\n  - {name: nodir, type: duckdb, path: '"+missingDir+"', read_only: false}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	assert.FileExists(t, existing, "an absolute path in your own scope is your explicit choice")
	require.Len(t, issues, 1)
	assert.Equal(t, "nodir", issues[0].Connection)
	assert.NoDirExists(t, filepath.Join(base, "not"), "folders are only ever created inside the workspace")
}

func TestSessionUserScopeRelativePathRefused(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	ws, err := Open(dir, WithUserConfig(UserConfig{Version: 1, Connections: []ConnectionDef{{Name: "u", Type: "duckdb", Path: "rel.duckdb"}}}))
	require.NoError(t, err)
	sess, issues, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "absolute")
}

func TestSessionEnvInterpolationAndMissingVariable(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "")
	writeLocal(t, ws, `connections:
  - {name: viaenv, type: duckdb, path: 'data/${DBNAME}.duckdb', read_only: false}
  - {name: broken, type: duckdb, path: 'data/${NOT_SET_ANYWHERE}.duckdb', read_only: false}
`)
	env := func(k string) (string, bool) { return "dbfile", k == "DBNAME" }
	sess, issues, err := OpenSession(context.Background(), ws, nil, WithSessionEnv(env))
	require.NoError(t, err)
	defer sess.Close()
	assert.FileExists(t, filepath.Join(dir, "data", "dbfile.duckdb"))
	require.Len(t, issues, 1, "a missing variable fails that connection only")
	assert.Equal(t, "broken", issues[0].Connection)
	assert.Contains(t, issues[0].Message, "NOT_SET_ANYWHERE")
}

func TestSessionUnavailableTypeIsAnIssueNotAFailure(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "default_connection: ghost\nconnections:\n  - {name: ghost, type: postgres, host: db}\n  - {name: ok, type: duckdb, path: data/ok.duckdb, read_only: false}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	assert.NotEmpty(t, issues)
	assert.NotEqual(t, "ghost", sess.DefaultCatalog())
	var okSeen bool
	for _, c := range sess.Connections() {
		okSeen = okSeen || c.Name == "ok"
	}
	assert.True(t, okSeen)
}

func TestSessionReadOnlyMissingFileIsAnIssue(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "connections:\n  - {name: ro, type: duckdb, path: data/nothere.duckdb}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil)
	require.NoError(t, err)
	defer sess.Close()
	require.Len(t, issues, 1)
	assert.Equal(t, "ro", issues[0].Connection)
	assert.NoFileExists(t, filepath.Join(dir, "data", "nothere.duckdb"), "a read-only connection never creates its file")
}

func TestDuplicateIDs(t *testing.T) {
	a, rootA := newWorkspace(t)
	rootB := filepath.Join(t.TempDir(), "copy")
	require.NoError(t, copyDir(rootA, rootB))
	b, err := Open(rootB, noUser(t))
	require.NoError(t, err)
	assert.Equal(t, a.ID(), b.ID(), "cp -r copies the id")

	known := Known{rootA: a.ID(), rootB: b.ID(), "/gone/elsewhere": a.ID(), "/other": "different-id"}
	assert.Equal(t, []string{rootA}, DuplicateIDs(rootB, b.ID(), known), "same id at another live path; deleted folders do not count")
	assert.Equal(t, []string{rootB}, DuplicateIDs(rootA, a.ID(), known))
	assert.Empty(t, DuplicateIDs(rootA, a.ID(), Known{rootA: a.ID()}), "the workspace itself is not a duplicate")

	// Regenerate id on the copy: new id, comments and the rest untouched.
	before, _ := os.ReadFile(filepath.Join(rootB, "vibe.yaml"))
	newID, err := RegenerateID(rootB)
	require.NoError(t, err)
	assert.NotEqual(t, a.ID(), newID)
	after, _ := os.ReadFile(filepath.Join(rootB, "vibe.yaml"))
	assert.Equal(t, strings.Replace(string(before), a.ID(), newID, 1), string(after))
	b2, err := Open(rootB, noUser(t))
	require.NoError(t, err)
	assert.Equal(t, newID, b2.ID())
	assert.Empty(t, DuplicateIDs(rootB, b2.ID(), Known{rootA: a.ID(), rootB: b2.ID()}))
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func TestWorkspaceServesItsStores(t *testing.T) {
	ws, _ := newWorkspace(t)
	nb, err := ws.Notebooks().Create(context.Background(), "t")
	require.NoError(t, err)
	got, _, err := ws.Notebooks().Get(context.Background(), nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "t", got.Title)
	assert.NotEmpty(t, ws.Policy().Sandbox)
}

func uuidParse(s string) (string, error) {
	_, err := uuid.Parse(s)
	return s, err
}

// A workspace is an agents.PolicySource as is: #111 hands it to the handler
// without an adapter.
var _ agents.PolicySource = Workspace(nil)

// Review #109 blocker 3: a skill saved from a stale copy is a conflict, not a
// silent clobber of whatever a git pull brought in meanwhile.
func TestSkillSaveFromStaleCopyIsAConflict(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	store := ws.Skills()
	require.NoError(t, saveSkill(store, nil, "foo", "first", "v1 body", ""))

	// Studio lists skills (and so learns each one's revision).
	entries, err := skills.LoadSkills(store, nil, nil)
	require.NoError(t, err)
	var loadedRev string
	for _, e := range entries {
		if e.Name == "foo" {
			loadedRev = e.Rev
		}
	}
	require.NotEmpty(t, loadedRev)

	// A teammate's edit arrives via git pull between load and save.
	p := filepath.Join(root, "skills", "foo", "SKILL.md")
	teammate := "---\nname: foo\ndescription: teammate\nlicense: MIT\n---\n\nTEAMMATE WORK\n"
	require.NoError(t, os.WriteFile(p, []byte(teammate), 0o644))

	// The editor saves with the revision it loaded.
	_, err = skills.SaveUserSkillAt(store, nil, "foo", "first", "v1 body edited", "", loadedRev)
	require.ErrorIs(t, err, skills.ErrConflict)
	data, _ := os.ReadFile(p)
	assert.Equal(t, teammate, string(data), "nothing was written")

	// Saving over an existing skill nothing has read (a fresh process) is also refused.
	fresh := skills.NewFileStore(root)
	err = saveSkill(fresh, nil, "foo", "x", "blind", "")
	assert.ErrorIs(t, err, skills.ErrConflict)

	// The agent path: it listed the skill, then a pull changed it, then it saves.
	entries, err = skills.LoadSkills(store, nil, nil)
	require.NoError(t, err)
	_ = entries
	require.NoError(t, os.WriteFile(p, []byte(strings.Replace(teammate, "TEAMMATE WORK", "SECOND PULL", 1)), 0o644))
	err = saveSkill(store, nil, "foo", "agent", "agent body", "")
	assert.ErrorIs(t, err, skills.ErrConflict, "the store's last-seen revision is used without a refresh")

	// Reload (list again) and the same save goes through, keeping unknown keys.
	_, err = skills.LoadSkills(store, nil, nil)
	require.NoError(t, err)
	require.NoError(t, saveSkill(store, nil, "foo", "agent", "agent body", ""))
	data, _ = os.ReadFile(p)
	assert.Contains(t, string(data), "license: MIT", "unknown frontmatter keys survive an agent save")
	assert.Contains(t, string(data), "agent body")

	// A new skill is just created.
	require.NoError(t, saveSkill(store, nil, "brand-new", "d", "body", ""))
	_, err = store.Get(ctx, "brand-new")
	assert.NoError(t, err)
}

// Review #109 probes turned into regression tests.

func TestNotebookStaleSaveWithAndWithoutRev(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	nb, err := ws.Notebooks().Create(ctx, "Sales")
	require.NoError(t, err)
	got, _, err := ws.Notebooks().Get(ctx, nb.ID)
	require.NoError(t, err)
	p := filepath.Join(root, "notebooks", nb.ID+".vnb")
	require.NoError(t, os.WriteFile(p, []byte(`{"schemaVersion":2,"title":"External","cells":[]}`+"\n"), 0o644))
	// The agent reads the notebook after the external change.
	_, _, err = ws.Notebooks().Get(ctx, nb.ID)
	require.NoError(t, err)
	// Studio saves its stale copy with the rev it loaded: a conflict, whatever
	// anyone else has read since.
	got.Title = "Stale"
	_, _, err = ws.Notebooks().Save(ctx, got)
	assert.ErrorIs(t, err, notebooks.ErrConflict)
	data, _ := os.ReadFile(p)
	assert.Contains(t, string(data), "External")
	// A caller that sends no rev falls back on the store's last-seen revision
	// (internal callers only): it has seen the external version, so it writes
	// on top of it, never on top of a version it never saw.
	got.Rev = ""
	got.Title = "Internal"
	_, _, err = ws.Notebooks().Save(ctx, got)
	assert.NoError(t, err)
	// A different store instance (another process) with no rev and no
	// history is refused outright.
	other := notebooks.NewFileStore(root)
	got.Rev, got.Title = "", "Blind"
	_, _, err = other.Save(ctx, got)
	assert.ErrorIs(t, err, notebooks.ErrConflict)
}

func TestNotebookHTMLCharactersRoundTripByteIdentically(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	nb, err := ws.Notebooks().Create(ctx, "H")
	require.NoError(t, err)
	nb, _, err = ws.Notebooks().Get(ctx, nb.ID)
	require.NoError(t, err)
	raw := map[string]any{"id": nb.ID, "rev": nb.Rev, "title": "a<b>&c \\u003c lit", "cells": []any{
		map[string]any{"id": "c1", "type": "markdown", "source": "x < y && y > z \\u003c \\\\u0026 </script>"},
		map[string]any{"id": "c2", "type": "sql", "source": "select '<&>' as \"a&b\""},
	}}
	s1, _, err := ws.Notebooks().Save(ctx, raw)
	require.NoError(t, err)
	b1, _ := os.ReadFile(filepath.Join(root, "notebooks", s1.ID+".vnb"))
	g, _, err := ws.Notebooks().Get(ctx, s1.ID)
	require.NoError(t, err)
	s2, _, err := ws.Notebooks().Save(ctx, g)
	require.NoError(t, err)
	b2, _ := os.ReadFile(filepath.Join(root, "notebooks", s2.ID+".vnb"))
	assert.Equal(t, string(b1), string(b2))
	assert.Equal(t, "a<b>&c \\u003c lit", g.Title, "a literal backslash-u sequence is data, not an escape")
	assert.Contains(t, string(b1), "x < y && y > z")
	assert.Equal(t, "x < y && y > z \\u003c \\\\u0026 </script>", g.Cells[0].Source)
}

func TestYAMLAliasBombIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("id: " + testID + "\nname: &a [\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\"]\n")
	prev := "a"
	for i := 0; i < 9; i++ {
		n := string(rune('b' + i))
		b.WriteString(n + ": &" + n + " [*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + "]\n")
		prev = n
	}
	start := time.Now()
	_, err := ParseConfig([]byte(b.String()))
	assert.Error(t, err, "unknown keys are rejected")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestDeleteAfterExternalEditIsAConflict(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	q := &queries.Query{Title: "Q", SQL: "select 1"}
	require.NoError(t, ws.Queries().Create(ctx, q))
	p := filepath.Join(root, "queries", q.ID+".sql")
	require.NoError(t, os.WriteFile(p, []byte("-- vibe: {\"title\":\"Q\"}\nselect 'teammate'\n"), 0o644))
	err := ws.Queries().Delete(ctx, q.ID)
	assert.ErrorIs(t, err, queries.ErrConflict)
	assert.FileExists(t, p, "a file changed since it was read is not deleted")
}

// Re-review #109 item 2: deleting a folder and cloning a different repository
// with the same id to the same path must not inherit the old settings.
func TestRecloneAtSamePathDoesNotInheritLocalSettings(t *testing.T) {
	cfg := t.TempDir()
	base := t.TempDir()
	p := filepath.Join(base, "proj")
	mk := func(extra string) {
		require.NoError(t, os.MkdirAll(p, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(p, "vibe.yaml"), []byte("id: "+testID+"\nsandbox: full\nconnections:\n  - {name: shared, type: duckdb, path: data/s.duckdb}\n"), 0o644))
		if extra != "" {
			require.NoError(t, os.MkdirAll(filepath.Join(p, ".vibe-local"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(p, ".vibe-local", "nonce"), []byte(extra+"\n"), 0o644))
		}
	}
	open := func() Workspace {
		ws, err := Open(p, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
		require.NoError(t, err)
		return ws
	}

	mk("")
	ws := open()
	assert.FileExists(t, filepath.Join(p, ".vibe-local", "nonce"), "the first open makes the folder's mark")
	require.NoError(t, ws.SaveLocal(LocalConfig{
		Agent:       LocalAgent{Sandbox: SandboxFull, AskBeforeWrites: Bool(false)},
		SignIn:      map[string]string{"shared": "env:MINE"},
		Connections: []ConnectionDef{{Name: "mine", Type: "duckdb", Path: "data/m.duckdb"}},
	}))
	assert.Equal(t, SandboxFull, ws.Policy().Sandbox)
	assert.True(t, ws.LocalTrusted())

	// The same folder opens again: still trusted.
	ws = open()
	assert.True(t, ws.LocalTrusted())
	assert.Equal(t, SandboxFull, ws.Policy().Sandbox)
	assert.Len(t, ws.Connections(), 2)

	for name, attackerNonce := range map[string]string{"no nonce in the new clone": "", "a nonce the attacker committed": "attacker-chosen"} {
		require.NoError(t, os.RemoveAll(p))
		mk(attackerNonce)
		ws = open()
		assert.False(t, ws.LocalTrusted(), name)
		assert.Equal(t, DefaultSandbox, ws.Policy().Sandbox, "%s: loosening not inherited", name)
		_, ask := ws.AgentPolicy()
		assert.True(t, ask, name)
		require.Len(t, ws.Connections(), 1, "%s: the old local connections are not inherited", name)
		assert.Equal(t, "keychain", ws.Connections()[0].SignIn, "%s: nor the sign-in methods", name)
		assert.Contains(t, strings.Join(ws.Warnings(), " "), "not the one your saved settings")
		assert.ErrorIs(t, ws.SaveLocal(LocalConfig{}), ErrLocalUntrusted, "saving would clobber settings that belong to another folder")
	}

	// Re-confirming brings them back.
	require.NoError(t, ws.TrustLocal())
	assert.True(t, ws.LocalTrusted())
	assert.Equal(t, SandboxFull, ws.Policy().Sandbox)
	assert.Len(t, ws.Connections(), 2)
	assert.Empty(t, ws.Warnings())
}

func TestHandWrittenLocalSettingsAreAdoptedAndStamped(t *testing.T) {
	cfg := t.TempDir()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	probe, err := Open(dir, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(probe.LocalConfigPath()), 0o700))
	require.NoError(t, os.WriteFile(probe.LocalConfigPath(), []byte("agent: {sandbox: read-only}\n"), 0o600))
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".vibe-local")))
	ws, err := Open(dir, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: 1}))
	require.NoError(t, err)
	assert.True(t, ws.LocalTrusted())
	assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox)
	assert.FileExists(t, filepath.Join(dir, ".vibe-local", "nonce"))
	require.NoError(t, ws.SaveLocal(ws.Local()))
	data, _ := os.ReadFile(ws.LocalConfigPath())
	assert.Contains(t, string(data), "nonce:")
}

func TestOpenSession_ForceReadOnly(t *testing.T) {
	ctx := context.Background()
	ws, root := newWorkspace(t)

	// Nothing written yet: a read-only session reports the missing data file
	// as an issue and creates nothing.
	sess, issues, err := OpenSession(ctx, ws, nil, WithForceReadOnly())
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "local", issues[0].Connection)
	assert.Contains(t, issues[0].Message, "no data file yet")
	require.NoError(t, sess.Close())
	assert.NoFileExists(t, filepath.Join(root, "data", "local.duckdb"))

	// With a file, everything is attached read-only whatever the definition
	// says (local is read_only: false).
	rw, issues, err := OpenSession(ctx, ws, nil)
	require.NoError(t, err)
	require.Empty(t, issues)
	_, err = rw.Exec(ctx, "CREATE TABLE t AS SELECT 1 AS id")
	require.NoError(t, err)
	require.NoError(t, rw.Close())

	ro, issues, err := OpenSession(ctx, ws, nil, WithForceReadOnly())
	require.NoError(t, err)
	defer ro.Close() //nolint:errcheck
	require.Empty(t, issues)
	var n int
	require.NoError(t, ro.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 1, n)
	_, err = ro.Exec(ctx, "INSERT INTO t VALUES (2)")
	require.Error(t, err)
	var found bool
	for _, st := range ro.Connections() {
		if st.Name == "local" {
			found = true
			assert.True(t, st.ReadOnly)
		}
	}
	assert.True(t, found)
}

// Opening a workspace never rewrites the folder's mark: a later open (another
// process, another run) must keep the settings stamped with it trusted.
func TestOpenNeverRewritesTheNonce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "n")
	res, err := Init(dir)
	require.NoError(t, err)
	root := res.Workspace.Root()
	require.NoError(t, res.Workspace.Close())
	cfg := t.TempDir()
	open := func() Workspace {
		ws, err := Open(root, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: ConfigVersion}))
		require.NoError(t, err)
		return ws
	}
	nonceFile := filepath.Join(root, ".vibe-local", "nonce")

	ws := open()
	first, err := os.ReadFile(nonceFile)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	for i := 0; i < 3; i++ { // no settings file yet: the old code re-stamped here
		require.NoError(t, ws.Close())
		ws = open()
		again, err := os.ReadFile(nonceFile)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(again), "open %d rewrote the nonce", i+2)
	}
	l := ws.Local()
	l.Agent.Sandbox = SandboxReadOnly
	require.NoError(t, ws.SaveLocal(l))
	require.NoError(t, ws.Close())
	for i := 0; i < 3; i++ {
		ws = open()
		again, err := os.ReadFile(nonceFile)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(again))
		assert.True(t, ws.LocalTrusted())
		assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox, "settings stay in force")
		require.NoError(t, ws.Close())
	}
}

// saveSkill is skills.SaveUserSkillAt with no revision check.
func saveSkill(store skills.Store, sess session.Session, name, description, instructions, tableName string) error {
	_, err := skills.SaveUserSkillAt(store, sess, name, description, instructions, tableName, "")
	return err
}

func TestInit_GitignoresDotEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	res, err := Init(dir)
	require.NoError(t, err)
	_ = res.Workspace.Close()
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	for _, l := range []string{".env", ".env.*", "!.env.example"} {
		assert.Contains(t, strings.Split(string(b), "\n"), l)
	}
}
