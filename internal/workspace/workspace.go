package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// Workspace is an open workspace folder: its configuration (committed, local
// and user scopes, merged), its file-backed stores and its watcher. It holds no
// DuckDB handle; OpenSession builds the session over it.
type Workspace interface {
	// Root is the absolute, symlink-resolved folder.
	Root() string
	// ID is the committed workspace id (keys keychain entries).
	ID() string
	// Name is the display name (the folder name when vibe.yaml has none).
	Name() string
	// Config is the committed vibe.yaml.
	Config() Config
	// Local is this person's own settings for this workspace (agent loosening,
	// sign-in methods, "This computer" connections). They live in the user
	// config directory, not in the folder; see LocalConfigPath.
	Local() LocalConfig
	// LocalConfigPath is the file Local is read from and SaveLocal writes.
	LocalConfigPath() string
	// LocalTrusted is false when the saved settings were made for a different
	// folder at this path (deleted and cloned again): Local is then empty and
	// SaveLocal refuses until TrustLocal re-confirms them.
	LocalTrusted() bool
	// TrustLocal re-confirms the saved settings for this folder.
	TrustLocal() error
	// Connections are the merged definitions, sorted by name, with provenance.
	Connections() []Connection
	// Connection is the merged definition called name.
	Connection(name string) (Connection, bool)
	// DefaultConnection is default_connection from vibe.yaml ("" if unset).
	DefaultConnection() string
	// Policy is the agent policy in effect for this person:
	// EffectivePolicy(committed ceiling, local loosening).
	Policy() Policy
	// AgentPolicy is Policy as plain values, the shape agents.PolicySource
	// asks for.
	AgentPolicy() (sandbox string, askBeforeWrites bool)
	// Warnings are non-fatal problems found while loading (for example a
	// default_connection that names nothing).
	Warnings() []string

	Notebooks() notebooks.Store
	Queries() queries.Store
	Skills() skills.Store

	// Resolver is the credential resolver (keychain, env, prompt) this
	// workspace uses. Never nil.
	Resolver() *secrets.Resolver

	// DotEnv describes the workspace's .env file: whether this person allowed
	// it, which variables it sets and which env: sign-ins they would feed.
	// Names only, never values. Allowed values are consulted by env: sign-ins
	// through the workspace's own overlay (never os.Setenv).
	DotEnv() DotEnvStatus
	// AllowDotEnv approves the .env as it is now and loads it. DenyDotEnv
	// records a no; RevokeDotEnv forgets either decision. Decisions live in the
	// user config directory, never in the folder.
	AllowDotEnv() error
	DenyDotEnv() error
	RevokeDotEnv() error
	// Connection CRUD (#115). Scope is where the definition lives: shared
	// (vibe.yaml, committed), local (your own file for this workspace) or user
	// (all your workspaces). Every definition is validated by the scope's rules
	// and its type's rules; none ever holds a credential.
	AddConnection(scope Scope, def ConnectionDef) error
	UpdateConnection(scope Scope, def ConnectionDef) error
	// RemoveConnection deletes the definition and, when nothing else keeps
	// using it, its sign-in choice and its saved credentials.
	RemoveConnection(ctx context.Context, scope Scope, name string) error
	// MoveConnection changes a definition's scope. The credential stays bound
	// to the workspace id, name and fingerprint; moving between a workspace
	// scope and the user scope migrates the keychain entry.
	MoveConnection(ctx context.Context, name string, from, to Scope) error
	// SetSignIn chooses this person's sign-in method for a connection
	// (keychain, prompt or env:PREFIX); it is stored outside the folder.
	SetSignIn(name, method string) error
	// RebindSignIn is SetSignIn for a person who confirmed the server changed:
	// SetSignIn refuses (ErrRebindNeedsConfirm) to bind an environment sign-in to
	// a server other than the one the saved sign-in was chosen for.
	RebindSignIn(name, method string) error
	// SetCredentials saves a credential for a connection under its sign-in
	// method, bound to the current fingerprint. Empty secret fields keep the
	// stored ones. Write-only: nothing here reads a secret back.
	SetCredentials(ctx context.Context, name string, cred secrets.Credential, remember bool) error
	ClearCredentials(ctx context.Context, name string) error
	// CredentialStatus describes the credential for the UI. It never carries a
	// secret.
	CredentialStatus(ctx context.Context, name string) (secrets.CredentialStatus, error)

	// Reload re-reads vibe.yaml and the local and user files. On error the
	// previous configuration stays in effect.
	Reload() error
	// SaveConfig writes vibe.yaml with compare-and-write against the version
	// last loaded; a file changed on disk since returns ErrConflict.
	SaveConfig(Config) error
	// SaveLocal writes the per-person settings file the same way.
	SaveLocal(LocalConfig) error
	// State and SaveState read and write .vibe-local/state.json (open tabs,
	// last notebook). A missing or unreadable file is an empty state; state is
	// never a reason to fail.
	State() State
	SaveState(State) error

	// Watch starts a file watcher over this workspace.
	Watch(opts ...WatchOption) (*Watcher, error)
	Close() error
}

// State is per-person, per-workspace UI state kept in .vibe-local/state.json.
type State struct {
	LastNotebook string   `json:"lastNotebook,omitempty"`
	OpenTabs     []string `json:"openTabs,omitempty"`
}

// ErrConflict is returned by SaveConfig / SaveLocal (wrapped) when the file
// changed on disk since it was loaded.
var ErrConflict = filestore.ErrConflict

// Option configures Open.
type Option func(*options)

type options struct {
	logger    *slog.Logger
	resolver  *secrets.Resolver
	user      *UserConfig // tests: replace the user scope instead of reading it
	configDir string      // "" = $VIBE_CONFIG_DIR or the user config dir
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithConfigDir sets the per-user config directory (default: $VIBE_CONFIG_DIR,
// else the OS user config dir + "/vibe"). The user-scope connections.yaml and
// the per-person workspace settings are read from and written under it.
func WithConfigDir(dir string) Option { return func(o *options) { o.configDir = dir } }

// WithResolver supplies the credential resolver (Studio shares one so every
// resolved secret reaches one redactor). The default is a resolver over the OS
// keychain with an environment snapshot taken at Open.
func WithResolver(r *secrets.Resolver) Option { return func(o *options) { o.resolver = r } }

// WithUserConfig supplies the user-scope config instead of reading the file in
// the config dir (tests, and callers that already loaded it).
func WithUserConfig(u UserConfig) Option { return func(o *options) { o.user = &u } }

type workspace struct {
	root      string
	opts      options
	logger    *slog.Logger
	configDir string

	nb notebooks.Store
	qs queries.Store
	sk skills.Store

	mu       sync.RWMutex
	cfg      Config
	cfgRev   string
	local    LocalConfig
	localRev string
	// localUntrusted is true when the per-person settings file was made for a
	// different folder at this path (nonce mismatch); they are not used.
	localUntrusted bool
	ownNonce       string // the nonce this process wrote, if any
	user           UserConfig
	userRev        string
	merged         []Connection
	warnings       []string

	denv dotenvState
	res  *secrets.Resolver // opts.resolver with the .env overlay layered on

	crudMu   sync.Mutex // serialises connection CRUD in this process
	wmu      sync.Mutex
	watchers []*Watcher
}

var _ Workspace = (*workspace)(nil)

// Open opens the workspace rooted at dir (the folder that holds vibe.yaml).
// Use Discover first to find it from a subdirectory.
func Open(dir string, opts ...Option) (Workspace, error) {
	o := options{logger: slog.Default()}
	for _, f := range opts {
		f(&o)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open workspace %q: %w", dir, err)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	w := &workspace{root: root, opts: o, logger: o.logger, configDir: o.configDir}
	if w.opts.resolver == nil {
		w.opts.resolver = secrets.NewResolver(secrets.WithEnv(SnapshotEnv()), secrets.WithResolverLogger(o.logger))
	}
	if w.configDir == "" {
		if d, err := prefs.DefaultDir(o.logger); err == nil {
			w.configDir = d
		}
	}
	if err := w.reload(); err != nil {
		return nil, err
	}
	w.initDotEnv(w.cfg.ID)
	filestore.SweepStaleTemps(root, w.tempDirs(), time.Minute)
	obs := w.observe
	w.nb = notebooks.NewFileStore(root, notebooks.WithObserver(obs))
	w.qs = queries.NewFileStore(root, queries.WithObserver(obs), queries.WithLogger(w.logger))
	w.sk = skills.NewFileStore(root, skills.WithObserver(obs), skills.WithLogger(w.logger))
	return w, nil
}

// observe forwards a store's own write to every live watcher.
func (w *workspace) observe(path, hash string) {
	w.wmu.Lock()
	ws := append([]*Watcher(nil), w.watchers...)
	w.wmu.Unlock()
	for _, x := range ws {
		x.noteWrite(path, hash)
	}
}

func (w *workspace) reload() error {
	data, st, err := filestore.Read(w.root, ConfigFile)
	if err != nil {
		if os.IsNotExist(err) || isNotFound(err) {
			return fmt.Errorf("%w: %s", ErrNotWorkspace, w.root)
		}
		return fmt.Errorf("failed to read %s: %w", ConfigFile, err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return err
	}

	ls, err := w.loadLocalScope(cfg)
	if err != nil {
		return err
	}
	warnings := ls.warnings
	// A connections.yaml inside the folder is whatever the repository
	// committed: it is never read (D-A), only reported.
	if _, _, ferr := filestore.Read(w.root, LocalDir+"/"+LocalConfigFile); ferr == nil || errors.Is(ferr, filestore.ErrTooLarge) {
		warnings = append(warnings, ".vibe-local/"+LocalConfigFile+" was ignored: settings that affect the agent, connections and sign-in are only read from your own config directory ("+w.localPath(cfg.ID)+")")
	}

	user, userWarnings, err := w.loadUserScope()
	if err != nil {
		return err
	}
	warnings = append(warnings, userWarnings...)

	merged := Merge(user, cfg, ls.local, cfg.DefaultConnection)
	if cfg.DefaultConnection != "" && !slices.ContainsFunc(merged, func(c Connection) bool { return c.Name == cfg.DefaultConnection }) {
		warnings = append(warnings, fmt.Sprintf("default_connection %q is not defined in any scope on this computer", cfg.DefaultConnection))
	}

	w.mu.Lock()
	w.cfg, w.cfgRev = cfg, st.Rev()
	w.local, w.localRev, w.localUntrusted = ls.local, ls.rev, ls.untrusted
	w.user, w.merged = user, merged
	w.userRev = w.readUserRev()
	w.warnings = warnings
	w.mu.Unlock()
	return nil
}

// localScope is the per-person settings read for one workspace.
type localScope struct {
	local     LocalConfig
	rev       string
	untrusted bool
	warnings  []string
}

// loadLocalScope reads this person's settings for the workspace from the
// config directory, ignoring them when they were made for another folder, and
// stamps the folder's nonce when it has none.
func (w *workspace) loadLocalScope(cfg Config) (localScope, error) {
	ls := localScope{local: LocalConfig{Version: ConfigVersion}}
	if w.configDir == "" {
		return ls, nil
	}
	ldata, found, err := readOptional(w.localPath(cfg.ID))
	if err != nil {
		return ls, fmt.Errorf("failed to read your settings for this workspace: %w", err)
	}
	if !found {
		// First open (or settings deleted): make this folder's mark once,
		// when it has none. An open never rewrites an existing mark:
		// another process may have stamped it into a settings file, and
		// changing it would make those settings look like another folder's.
		if w.readNonce() == "" {
			w.writeNonce()
		}
		return ls, nil
	}
	local, err := ParseLocalConfig(ldata)
	if err != nil {
		return ls, fmt.Errorf("%s: %w", w.localPath(cfg.ID), err)
	}
	ls.local, ls.rev = local, filestore.HashBytes(ldata)
	folder := w.readNonce()
	switch {
	case local.Nonce != "" && local.Nonce != folder:
		// Same id at the same path, but not the folder these settings
		// were made for (deleted and cloned again, perhaps from a
		// different repository): trust nothing in them.
		ls.untrusted = true
		ls.local = LocalConfig{Version: ConfigVersion}
		ls.warnings = append(ls.warnings, "this folder is not the one your saved settings for this workspace were made for, so they were ignored (agent limits you loosened, sign-in methods, your own connections); confirm them with TrustLocal if it is the same project")
	case local.Nonce == "" && folder == "":
		w.writeNonce() // an older or hand-written file: adopt this folder
	}
	return ls, nil
}

// loadUserScope reads the all-workspaces connections, from the injected value
// or the user file.
func (w *workspace) loadUserScope() (UserConfig, []string, error) {
	if w.opts.user != nil {
		user := *w.opts.user
		if err := user.Validate(); err != nil {
			return UserConfig{}, nil, err
		}
		return user, nil, nil
	}
	user, err := w.loadUser()
	if err != nil {
		// A broken user file must not make every workspace unopenable: skip
		// that scope and say so.
		w.logger.Warn("ignoring unreadable user connections file", "error", err)
		return UserConfig{Version: ConfigVersion}, []string{"your connections.yaml could not be read and was ignored: " + err.Error()}, nil
	}
	return user, nil, nil
}

func isNotFound(err error) bool {
	return err != nil && (os.IsNotExist(err) || errors.Is(err, filestore.ErrNotFound))
}

func (w *workspace) Root() string { return w.root }

func (w *workspace) ID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg.ID
}

func (w *workspace) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.cfg.Name != "" {
		return w.cfg.Name
	}
	return filepath.Base(w.root)
}

func (w *workspace) Config() Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	c := w.cfg
	c.Connections = append([]ConnectionDef(nil), c.Connections...)
	return c
}

func (w *workspace) Local() LocalConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	l := w.local
	l.Connections = append([]ConnectionDef(nil), l.Connections...)
	return l
}

func (w *workspace) Connections() []Connection {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]Connection(nil), w.merged...)
}

func (w *workspace) DefaultConnection() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg.DefaultConnection
}

func (w *workspace) Policy() Policy {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return EffectivePolicy(CeilingOf(w.cfg), w.local.Agent)
}

func (w *workspace) AgentPolicy() (string, bool) {
	p := w.Policy()
	return p.Sandbox, p.AskBeforeWrites
}

func (w *workspace) Warnings() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]string(nil), w.warnings...)
}

func (w *workspace) Notebooks() notebooks.Store { return w.nb }
func (w *workspace) Queries() queries.Store     { return w.qs }
func (w *workspace) Skills() skills.Store       { return w.sk }

func (w *workspace) Reload() error { return w.reload() }

func (w *workspace) SaveConfig(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := MarshalConfig(c)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := filestore.WriteAtomic(w.root, filestore.Write{Rel: ConfigFile, Data: data, Expect: w.cfgRev}, w.observe); err != nil {
		return fmt.Errorf("failed to save %s: %w", ConfigFile, err)
	}
	w.cfg, w.cfgRev = c, filestore.HashBytes(data)
	w.merged = Merge(w.user, w.cfg, w.local, w.cfg.DefaultConnection)
	return nil
}

// ErrLocalUntrusted is returned by SaveLocal while the saved per-person
// settings belong to a different folder; TrustLocal re-confirms them.
var ErrLocalUntrusted = errors.New("your saved settings for this workspace were made for a different folder; confirm them before changing them")

func (w *workspace) SaveLocal(l LocalConfig) error {
	if w.configDir == "" {
		return fmt.Errorf("no per-user config directory is available to keep your settings in")
	}
	if err := l.Validate(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.localUntrusted {
		return ErrLocalUntrusted
	}
	if n := w.readNonce(); n != "" {
		l.Nonce = n
	} else if n := w.writeNonce(); n != "" {
		l.Nonce = n
	}
	data, err := MarshalLocalConfig(l)
	if err != nil {
		return err
	}
	lroot, lrel := w.localLocation(w.cfg.ID)
	if err := os.MkdirAll(lroot, 0o700); err != nil {
		return fmt.Errorf("failed to create your settings directory: %w", err)
	}
	if _, err := filestore.WriteAtomic(lroot, filestore.Write{Rel: lrel, Data: data, Expect: w.localRev}, nil); err != nil {
		return fmt.Errorf("failed to save your settings for this workspace: %w", err)
	}
	w.local, w.localRev = l, filestore.HashBytes(data)
	w.merged = Merge(w.user, w.cfg, w.local, w.cfg.DefaultConnection)
	return nil
}

func (w *workspace) State() State {
	data, _, err := filestore.Read(w.root, LocalDir+"/"+LocalStateFile)
	if err != nil {
		return State{}
	}
	var s State
	if json.Unmarshal(data, &s) != nil {
		return State{}
	}
	return s
}

func (w *workspace) SaveState(s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode state: %w", err)
	}
	data = append(data, '\n')
	if _, err := filestore.WriteAtomic(w.root, filestore.Write{Rel: LocalDir + "/" + LocalStateFile, Data: data, Overwrite: true}, nil); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}
	return nil
}

func (w *workspace) Close() error {
	w.wmu.Lock()
	ws := w.watchers
	w.watchers = nil
	w.wmu.Unlock()
	var first error
	for _, x := range ws {
		if err := x.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// localLocation splits the per-person settings path into the directory used as
// the filestore root and the file name within it.
func (w *workspace) localLocation(id string) (root, rel string) {
	p := LocalConfigPath(w.configDir, id, w.root)
	return filepath.Dir(p), filepath.Base(p)
}

func (w *workspace) localPath(id string) string { return LocalConfigPath(w.configDir, id, w.root) }

func (w *workspace) loadUser() (UserConfig, error) {
	if w.configDir == "" {
		return UserConfig{Version: ConfigVersion}, nil
	}
	return LoadUserConfigFrom(w.configDir)
}

// tempDirs lists the directories WriteAtomic leaves temp files in.
func (w *workspace) tempDirs() []string {
	dirs := []string{".", notebooks.Dir, queries.Dir, skills.Dir, LocalDir}
	if subs, err := filestore.ListSubdirs(w.root, skills.Dir); err == nil {
		for _, s := range subs {
			dirs = append(dirs, skills.Dir+"/"+s)
		}
	}
	return dirs
}

func (w *workspace) LocalConfigPath() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.localPath(w.cfg.ID)
}

// nonceRel is the folder's mark, kept in the git-ignored .vibe-local/. The
// per-person settings file records the same value; a mismatch means the folder
// at this path is not the one the settings were made for.
const nonceRel = LocalDir + "/nonce"

func (w *workspace) readNonce() string {
	data, _, err := filestore.Read(w.root, nonceRel)
	if err != nil {
		return ""
	}
	n := strings.TrimSpace(string(data))
	if len(n) > 128 {
		return ""
	}
	return n
}

// writeNonce stores a fresh random nonce and returns it ("" if the folder is
// not writable, in which case nothing can be trusted to match later).
func (w *workspace) writeNonce() string {
	n := filestore.RandomSuffix(32)
	if _, err := filestore.WriteAtomic(w.root, filestore.Write{Rel: nonceRel, Data: []byte(n + "\n"), Overwrite: true}, nil); err != nil {
		w.logger.Warn("could not record this folder's mark", "error", err)
		return ""
	}
	w.ownNonce = n
	return n
}

func (w *workspace) LocalTrusted() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return !w.localUntrusted
}

func (w *workspace) TrustLocal() error {
	w.mu.Lock()
	ldata, found, err := readOptional(w.localPath(w.cfg.ID))
	if err != nil || !found {
		w.mu.Unlock()
		return err
	}
	l, perr := ParseLocalConfig(ldata)
	if perr != nil {
		w.mu.Unlock()
		return perr
	}
	if l.Nonce != "" {
		if _, werr := filestore.WriteAtomic(w.root, filestore.Write{Rel: nonceRel, Data: []byte(l.Nonce + "\n"), Overwrite: true}, nil); werr != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to confirm your settings: %w", werr)
		}
	}
	w.mu.Unlock()
	return w.reload()
}
