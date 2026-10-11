package main

// Workspace lifecycle and bindings (#111): the launcher's recent list, open /
// create / switch / close, the hub lock (one writer per workspace), the file
// watcher, the workspace settings and Studio's own MCP switch.
//
// Everything here follows the order the hub requires:
//
//	open:  workspace.Open -> hub.AcquireHub -> workspace.OpenSession
//	close: MCP stop -> watcher stop -> session close -> hub release -> workspace close
//
// The caller of every *Locked function holds a.mu for writing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/llm/skills"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/templates"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// maxRecentWorkspaces caps the launcher's list.
const maxRecentWorkspaces = 12

// prefsRecentKey is where the recent list lives in the preferences document
// (prefs.Preferences.Extra), outside every workspace.
const prefsRecentKey = "recentWorkspaces"

// releaseGrace is how long a hub release request waits, after telling the
// frontend, for it to flush open notebooks before the workspace is closed.
const releaseGrace = 400 * time.Millisecond

// legacyVibeMessage is what Studio says about a .vibe file.
const legacyVibeMessage = ".vibe files are bundles: use Import .vibe… to bring one into a workspace. Open a workspace folder to work in it."

// conflictPrefix starts the message of a save refused because the file changed
// on disk; the frontend matches it to show the conflict banner.
const conflictPrefix = "conflict: "

// conflictErr marks a filestore conflict so the UI can tell it from any other
// save failure. Other errors pass through untouched.
func conflictErr(err error) error {
	if err != nil && errors.Is(err, filestore.ErrConflict) {
		var ce *filestore.ConflictError
		if errors.As(err, &ce) {
			return errors.New(conflictPrefix + ce.Error())
		}
		return errors.New(conflictPrefix + "the file changed on disk since it was loaded")
	}
	return err
}

// ---- types ----

// RecentWorkspace is one entry of the launcher's list.
type RecentWorkspace struct {
	Path       string `json:"path"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastOpened string `json:"lastOpened"` // RFC 3339
	GitBranch  string `json:"gitBranch"`
	// Missing is true when the folder (or its vibe.yaml) is gone: an unplugged
	// drive, a deleted folder. The entry is never removed automatically.
	Missing bool `json:"missing"`
}

// ConnectionInfo is one connection as the workspace screens show it.
type ConnectionInfo struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Scope       string   `json:"scope"` // shared | local | user (the UI says Team / This computer / All my workspaces)
	Shadows     []string `json:"shadows,omitempty"`
	ReadOnly    bool     `json:"readOnly"`
	Default     bool     `json:"default"`
	Status      string   `json:"status"`
	Detail      string   `json:"detail,omitempty"`
	LockedByPID int      `json:"lockedByPid,omitempty"`
	// Remote connections dial on first use (Postgres, MotherDuck, storage);
	// Catalogless ones (s3, gcs, r2) have no tables, only a scoped secret.
	Remote      bool `json:"remote"`
	Catalogless bool `json:"catalogless,omitempty"`
	// SignIn is this person's sign-in method (keychain, prompt or env:PREFIX).
	SignIn string `json:"signIn,omitempty"`
	// Extension names the DuckDB extension an extension-missing connection
	// needs, for the install prompt.
	Extension string `json:"extension,omitempty"`
	// ServerChanged is set when a needs-credentials status is because the
	// definition now points at a different server than the saved sign-in.
	ServerChanged *ServerChange `json:"serverChanged,omitempty"`
}

// WorkspaceCounts are the live counts shown on Home.
type WorkspaceCounts struct {
	Notebooks int `json:"notebooks"`
	Queries   int `json:"queries"`
	Skills    int `json:"skills"`
	Tables    int `json:"tables"`
}

// AgentPolicyInfo is a sandbox tier plus the ask-before-changes gate.
type AgentPolicyInfo struct {
	Sandbox         string `json:"sandbox"`
	AskBeforeWrites bool   `json:"askBeforeWrites"`
}

// HolderInfo says which process owns the workspace's hub.
type HolderInfo struct {
	App       string `json:"app"`
	PID       int    `json:"pid"`
	ServesMCP bool   `json:"servesMcp"`
	Text      string `json:"text"` // "vibe mcp serve (pid 4182)"
}

// WorkspaceInfo is everything the shell needs to describe the open workspace.
type WorkspaceInfo struct {
	Root      string `json:"root"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	// GitBranch is read from .git/HEAD (no git process); empty when the
	// folder is not a repository.
	GitBranch         string           `json:"gitBranch"`
	IsGit             bool             `json:"isGit"`
	DefaultConnection string           `json:"defaultConnection"`
	Connections       []ConnectionInfo `json:"connections"`
	Counts            WorkspaceCounts  `json:"counts"`
	// Policy is in effect for this person; Team is the committed limit alone;
	// Local is this person's own choice.
	Policy AgentPolicyInfo `json:"policy"`
	Team   AgentPolicyInfo `json:"team"`
	Local  AgentPolicyInfo `json:"local"`
	// TeamSandboxSet is false when vibe.yaml sets no tier limit.
	TeamSandboxSet bool `json:"teamSandboxSet"`
	// ReadOnly is true when another process owns the hub.
	ReadOnly bool                     `json:"readOnly"`
	Holder   *HolderInfo              `json:"holder,omitempty"`
	Issues   []workspace.SessionIssue `json:"issues"`
	Warnings []string                 `json:"warnings"`
	// DuplicateIDs lists other known folders that carry this workspace's id (a
	// copied folder): offer "Regenerate id".
	DuplicateIDs []string `json:"duplicateIds"`
	// LocalUntrusted is true when the saved per-person settings were made for a
	// different folder at this path: they are ignored until confirmed.
	LocalUntrusted bool `json:"localUntrusted"`
	MCPRunning     bool `json:"mcpRunning"`
}

// OpenWorkspaceResult is what open, create and switch return.
type OpenWorkspaceResult struct {
	// Status is "opened" or "not-workspace" (the folder has no vibe.yaml: the
	// UI asks "Make this folder a workspace?" and calls CreateWorkspace in
	// place).
	Status   string         `json:"status"`
	Path     string         `json:"path"`
	Info     *WorkspaceInfo `json:"info,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

// InitialWorkspace is the folder named on the command line, if any.
type InitialWorkspace struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	// BundlePath is a .vibe bundle passed on the command line (the OS opened
	// one with Vibe): the launcher offers to import it.
	BundlePath string `json:"bundlePath"`
}

// CreateWorkspaceRequest asks for a new workspace.
type CreateWorkspaceRequest struct {
	Name string `json:"name"`
	// Dir is the folder that becomes the workspace. Empty means a new folder
	// named after Name inside ParentDir.
	Dir       string `json:"dir"`
	ParentDir string `json:"parentDir"`
	Git       bool   `json:"git"`
	// Template is a starter template name ("" is blank).
	Template string `json:"template"`
	// InPlace initialises a folder that already has files ("Make this folder a
	// workspace?"); without it a non-empty folder is refused.
	InPlace bool `json:"inPlace"`
}

// WorkspaceDirCheck answers "where will this go, and is it free?".
type WorkspaceDirCheck struct {
	Dir      string `json:"dir"`
	Exists   bool   `json:"exists"`
	NotEmpty bool   `json:"notEmpty"`
}

// WorkspaceSettings is the Settings > Workspace form.
type WorkspaceSettings struct {
	Name              string `json:"name"`
	DefaultConnection string `json:"defaultConnection"`
	// TeamSandbox and TeamAskBeforeChanges are the committed limit (vibe.yaml).
	// "" or "full" means no tier limit; the gate flag true means required for
	// everyone.
	TeamSandbox          string `json:"teamSandbox"`
	TeamAskBeforeChanges bool   `json:"teamAskBeforeChanges"`
	// Sandbox and AskBeforeChanges are this person's own choice (kept outside
	// the folder).
	Sandbox          string `json:"sandbox"`
	AskBeforeChanges bool   `json:"askBeforeChanges"`
	// LoadedTeamSandbox and LoadedTeamAsk are the committed values the form was
	// opened with. When sent, a vibe.yaml that has changed since (a git pull) makes
	// the save refuse instead of silently overwriting the team's change.
	LoadedTeamSandbox *string `json:"loadedTeamSandbox,omitempty"`
	LoadedTeamAsk     *bool   `json:"loadedTeamAsk,omitempty"`
}

// ConflictRequest resolves a save refused because the file changed on disk.
type ConflictRequest struct {
	Kind   string `json:"kind"`   // notebook | query | skill
	ID     string `json:"id"`     // notebook id, query id or skill name
	Action string `json:"action"` // reload | overwrite | saveCopy
	// Data is the local edit. Notebook: the same document SaveNotebook takes.
	// Query: {title, sql, description}. Skill: {description, instructions,
	// tableName}.
	Data map[string]any `json:"data"`
	// Rev is the revision on disk that the conflict banner was raised against
	// (notebooks and queries). "Keep mine" only overwrites that revision: a file that
	// changed again meanwhile is a fresh conflict.
	Rev string `json:"rev"`
	// Connection (queries) is the editor's current "Run on" choice, which "Keep
	// mine" and "Save as copy" write. Absent keeps the connection on disk; ""
	// is the workspace default.
	Connection *string `json:"connection,omitempty"`
}

// ConflictResult says where the resolved item lives now.
type ConflictResult struct {
	ID    string `json:"id"`
	Rev   string `json:"rev"`
	Title string `json:"title"`
}

// MCPStatus is Studio's own MCP switch. It never carries the bearer token.
type MCPStatus struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	// CanStart is false when this window cannot serve MCP (read-only, or no
	// workspace); Reason says why.
	CanStart bool   `json:"canStart"`
	Reason   string `json:"reason"`
}

// CatalogTable is one table of one connection.
type CatalogTable struct {
	Name      string `json:"name"`
	Qualified string `json:"qualified"`
	Kind      string `json:"kind"` // table | view
}

// CatalogGroup is one connection and its tables, for the Tables view.
type CatalogGroup struct {
	Connection string         `json:"connection"`
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	Detail     string         `json:"detail,omitempty"`
	ReadOnly   bool           `json:"readOnly"`
	Default    bool           `json:"default"`
	Tables     []CatalogTable `json:"tables"`
}

// ---- startup argument ----

// launchArgPath turns what the OS hands a launcher into a plain path. A desktop
// entry with Exec=vibe-studio %U passes file:// URIs (percent-encoded), and
// macOS passes plain paths.
func launchArgPath(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(strings.ToLower(arg), "file:") {
		if u, err := url.Parse(arg); err == nil && u.Path != "" && (u.Host == "" || u.Host == "localhost") {
			return u.Path
		}
	}
	return arg
}

// classifyLaunchArg decides what a path handed to Studio at launch means: a
// workspace folder (Path), a .vibe bundle to import (BundlePath), or a plain
// note when it is neither. An empty result means "ignore" (a flag, or nothing).
func classifyLaunchArg(arg string) InitialWorkspace {
	arg = launchArgPath(arg)
	if arg == "" || strings.HasPrefix(arg, "-") {
		return InitialWorkspace{}
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return InitialWorkspace{Message: "Couldn't read " + arg + "."}
	}
	fi, err := os.Stat(abs)
	switch {
	case err == nil && fi.IsDir():
		// A folder is a workspace whatever its name (even x.vibe).
		return InitialWorkspace{Path: abs}
	case strings.EqualFold(filepath.Ext(abs), ".vibe"):
		// A bundle (#118). A file that is not a v2 bundle is told so by the
		// import preview, not here.
		return InitialWorkspace{BundlePath: abs}
	case err != nil:
		return InitialWorkspace{Message: "The folder " + abs + " isn't there."}
	default:
		return InitialWorkspace{Message: abs + " is a file, not a folder. Open a workspace folder, or a .vibe bundle to import it."}
	}
}

// resolveLaunch decides what Studio opens at startup. args are os.Args[1:].
//
//   - The first argument, if a path, wins. A folder holding a vibe.yaml is that workspace; a
//     folder inside a workspace opens the enclosing workspace; any other
//     folder is passed through unchanged (the open flow offers to make it one).
//     A .vibe file, a missing path or a file gets classifyLaunchArg's answer.
//   - With no usable argument, a terminal start opens the workspace found by
//     walking up from cwd (workspace.DiscoverForLaunch, which never selects
//     home, an ancestor of home or /). A desktop-launcher start (cwd is home
//     or /) finds none and yields the zero value: the Launcher, as before.
func resolveLaunch(args []string, cwd, home string) InitialWorkspace {
	// Only the first argument counts (as before); a flag there means "none".
	if len(args) > 0 {
		if r := classifyLaunchArg(args[0]); r != (InitialWorkspace{}) {
			if r.Path != "" && !hasWorkspaceFile(r.Path) {
				if root, err := workspace.DiscoverForLaunch(r.Path, home); err == nil {
					r.Path = root
				}
			}
			return r
		}
	}
	if cwd == "" {
		return InitialWorkspace{}
	}
	if root, err := workspace.DiscoverForLaunch(cwd, home); err == nil {
		return InitialWorkspace{Path: root}
	}
	return InitialWorkspace{}
}

// setLaunchFromEnvironment records what to open from the command line and the
// working directory.
func (a *App) setLaunchFromEnvironment(args []string) {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	if r := resolveLaunch(args, cwd, home); r != (InitialWorkspace{}) {
		a.launchMu.Lock()
		a.initial = r
		a.launchMu.Unlock()
	}
}

// handleOpenPath is the macOS OnFileOpen entry (an Apple Event, not argv): the
// Finder opened a folder or a .vibe file with Vibe. Before the frontend has
// asked for its launch state the path is queued (the last one wins, as with
// argv); afterwards it is sent as an "app:open-path" event carrying the same
// InitialWorkspace shape GetInitialWorkspace returns.
func (a *App) handleOpenPath(path string) {
	r := classifyLaunchArg(path)
	if r == (InitialWorkspace{}) {
		return
	}
	a.launchMu.Lock()
	ready := a.frontendReady
	if !ready {
		a.initial = r
	}
	a.launchMu.Unlock()
	if ready {
		a.emitEvent("app:open-path", r)
	}
}

// GetInitialWorkspace returns what was named on the command line (or opened
// from the OS before the window was ready): a folder, a bundle, or a note when
// the argument could not be used. Calling it marks the frontend ready, so later
// OS open requests arrive as "app:open-path" events instead.
func (a *App) GetInitialWorkspace() InitialWorkspace {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	a.frontendReady = true
	return a.initial
}

// ---- recents ----

type recentRecord struct {
	Path       string `json:"path"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastOpened string `json:"lastOpened"`
}

var recentsMu sync.Mutex

func (a *App) loadRecentsLocked() (prefs.Preferences, []recentRecord) {
	if a.prefs == nil {
		return prefs.Defaults(), nil
	}
	p, err := a.prefs.Load()
	if err != nil {
		a.logger.Warn("recent workspaces: preferences unreadable", "error", err)
	}
	var recs []recentRecord
	if raw, ok := p.Extra[prefsRecentKey]; ok {
		if err := json.Unmarshal(raw, &recs); err != nil {
			a.logger.Warn("recent workspaces: list unreadable, starting over", "error", err)
			recs = nil
		}
	}
	return p, recs
}

func (a *App) saveRecentsLocked(p prefs.Preferences, recs []recentRecord) error {
	if a.prefs == nil {
		return nil
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	if len(recs) > maxRecentWorkspaces {
		recs = recs[:maxRecentWorkspaces]
	}
	raw, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[prefsRecentKey] = raw
	return a.prefs.Save(p)
}

// recordRecent puts root at the top of the list.
func (a *App) recordRecent(root, id, name string) {
	recentsMu.Lock()
	defer recentsMu.Unlock()
	p, recs := a.loadRecentsLocked()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	found := false
	for i := range recs {
		if samePath(recs[i].Path, root) {
			recs[i].Path, recs[i].ID, recs[i].Name, recs[i].LastOpened = root, id, name, now
			found = true
		}
	}
	if !found {
		recs = append(recs, recentRecord{Path: root, ID: id, Name: name, LastOpened: now})
	}
	if err := a.saveRecentsLocked(p, recs); err != nil {
		a.logger.Warn("failed to remember the workspace", "error", err)
	}
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func hasWorkspaceFile(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, workspace.ConfigFile))
	return err == nil && fi.Mode().IsRegular()
}

// ListRecentWorkspaces returns the launcher's list, most recent first. It only
// looks at the disk; nothing is opened or dialled.
func (a *App) ListRecentWorkspaces() ([]RecentWorkspace, error) {
	recentsMu.Lock()
	_, recs := a.loadRecentsLocked()
	recentsMu.Unlock()
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	out := make([]RecentWorkspace, 0, len(recs))
	for _, r := range recs {
		missing := !hasWorkspaceFile(r.Path)
		branch := ""
		if !missing {
			branch, _ = gitBranch(r.Path)
		}
		out = append(out, RecentWorkspace{
			Path: r.Path, ID: r.ID, Name: r.Name, LastOpened: r.LastOpened,
			GitBranch: branch, Missing: missing,
		})
	}
	return out, nil
}

// RemoveRecentWorkspace drops an entry from the list (never touching files) and
// returns it so the UI can offer Undo.
func (a *App) RemoveRecentWorkspace(path string) (*RecentWorkspace, error) {
	recentsMu.Lock()
	defer recentsMu.Unlock()
	p, recs := a.loadRecentsLocked()
	var removed *RecentWorkspace
	kept := recs[:0:0]
	for _, r := range recs {
		if samePath(r.Path, path) && removed == nil {
			removed = &RecentWorkspace{Path: r.Path, ID: r.ID, Name: r.Name, LastOpened: r.LastOpened}
			continue
		}
		kept = append(kept, r)
	}
	if err := a.saveRecentsLocked(p, kept); err != nil {
		return nil, fmt.Errorf("failed to update the list: %w", err)
	}
	return removed, nil
}

// RestoreRecentWorkspace puts back an entry RemoveRecentWorkspace returned
// (Undo).
func (a *App) RestoreRecentWorkspace(r RecentWorkspace) error {
	if strings.TrimSpace(r.Path) == "" {
		return errors.New("nothing to restore")
	}
	recentsMu.Lock()
	defer recentsMu.Unlock()
	p, recs := a.loadRecentsLocked()
	for _, e := range recs {
		if samePath(e.Path, r.Path) {
			return nil
		}
	}
	if r.LastOpened == "" {
		r.LastOpened = time.Now().UTC().Format(time.RFC3339Nano)
	}
	recs = append(recs, recentRecord{Path: r.Path, ID: r.ID, Name: r.Name, LastOpened: r.LastOpened})
	return a.saveRecentsLocked(p, recs)
}

// RelocateRecentWorkspace re-points a missing entry at the folder the person
// found ("Locate…").
func (a *App) RelocateRecentWorkspace(oldPath, newPath string) error {
	root, err := workspace.Discover(newPath)
	if err != nil || !samePath(root, newPath) {
		return errors.New("That folder isn't a Vibe workspace (it has no vibe.yaml).")
	}
	recentsMu.Lock()
	defer recentsMu.Unlock()
	p, recs := a.loadRecentsLocked()
	for i := range recs {
		if samePath(recs[i].Path, oldPath) || recs[i].Path == oldPath {
			recs[i].Path = root
		}
	}
	return a.saveRecentsLocked(p, recs)
}

// knownIDs maps every remembered folder to its id.
func (a *App) knownIDs() workspace.Known {
	recentsMu.Lock()
	defer recentsMu.Unlock()
	_, recs := a.loadRecentsLocked()
	k := workspace.Known{}
	for _, r := range recs {
		if r.ID != "" {
			k[r.Path] = r.ID
		}
	}
	return k
}

// ---- dialogs ----

// ChooseFolder opens the native folder picker.
func (a *App) ChooseFolder(title string) (string, error) {
	if title == "" {
		title = "Choose a folder"
	}
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	if err != nil {
		return "", fmt.Errorf("failed to open the folder dialog: %w", err)
	}
	return path, nil
}

// SelectWorkspaceFolder opens the native folder picker for "Open folder…".
func (a *App) SelectWorkspaceFolder() (string, error) {
	return a.ChooseFolder("Open a workspace folder")
}

// CheckGitAvailable reports whether git is on PATH.
func (a *App) CheckGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ---- git (pure Go, never runs git) ----

// gitBranch reads the branch from .git/HEAD without running git: a repository's
// own config (core.fsmonitor, filters) is never executed on open.
func gitBranch(root string) (branch string, isRepo bool) {
	gitPath := filepath.Join(root, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	headDir := gitPath
	if !fi.IsDir() { // a worktree or submodule: ".git" is a file "gitdir: <path>"
		b, err := os.ReadFile(gitPath)
		if err != nil {
			return "", true
		}
		line := strings.TrimSpace(string(b))
		if !strings.HasPrefix(line, "gitdir:") {
			return "", true
		}
		headDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(headDir) {
			headDir = filepath.Join(root, headDir)
		}
	}
	b, err := os.ReadFile(filepath.Join(headDir, "HEAD"))
	if err != nil {
		return "", true
	}
	head := strings.TrimSpace(string(b))
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/"), true
	}
	if len(head) > 7 {
		head = head[:7]
	}
	return head, true
}

// ---- open / close ----

type openMode int

const (
	openAuto     openMode = iota // take the hub; read-only when someone else has it
	openReadOnly                 // never take the hub
	openTakeOver                 // ask the current owner to let go, then take it
)

// OpenWorkspace opens the workspace folder at path (or the one that contains
// it). When another process owns the workspace's database the folder opens
// read-only and the event "workspace:locked" says who; the UI offers Take
// over. A folder with no vibe.yaml returns Status "not-workspace": the UI asks
// before calling CreateWorkspace in place.
func (a *App) OpenWorkspace(path string) (*OpenWorkspaceResult, error) {
	return a.open(path, openAuto)
}

func (a *App) open(path string, mode openMode) (*OpenWorkspaceResult, error) {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Ext(path), ".vibe") {
		return nil, errors.New(legacyVibeMessage)
	}
	if path == "" {
		return nil, errors.New("Choose a folder to open.")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("The folder %s isn't there. It may be on a drive that isn't plugged in.", path)
	}
	if !fi.IsDir() {
		return nil, errors.New("That is a file, not a folder. Open a workspace folder.")
	}
	// A pending approval card ends right now as "cancelled, nothing changed"
	// instead of waiting out its timeout on a workspace that is being swapped.
	// It runs again under the lock to close the window where a Wait registers
	// between the first call and taking the lock.
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	return a.openLocked(path, mode)
}

func (a *App) openLocked(path string, mode openMode) (*OpenWorkspaceResult, error) {
	root, err := workspace.Discover(path)
	if errors.Is(err, workspace.ErrNotWorkspace) {
		abs, _ := filepath.Abs(path)
		return &OpenWorkspaceResult{Status: "not-workspace", Path: abs}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look for a workspace in %s: %w", path, err)
	}

	// Parse and validate the new workspace BEFORE letting go of the current one,
	// so a broken folder never drops what is open. (workspace.Open takes no hub
	// lock; the one-releasable-hold rule only bites at AcquireHub below.)
	ws, err := workspace.Open(root, workspace.WithLogger(a.logger), workspace.WithResolver(a.secrets))
	if errors.Is(err, workspace.ErrMissingID) {
		// A hand-written vibe.yaml, or a template copied without an id.
		if _, ierr := workspace.EnsureID(root); ierr != nil {
			return nil, fmt.Errorf("failed to give this workspace an id: %w", ierr)
		}
		ws, err = workspace.Open(root, workspace.WithLogger(a.logger), workspace.WithResolver(a.secrets))
	}
	if err != nil {
		return nil, fmt.Errorf("Couldn't open this workspace: %w", err)
	}
	a.closeWorkspaceLocked("switch")

	var (
		hold     *hub.Hold
		holder   *hub.Holder
		readOnly bool
	)
	switch mode {
	case openReadOnly:
		readOnly = true
		holder, _ = hub.ReadHolder(root)
	case openTakeOver:
		hold, err = hub.TakeOver(a.ctxOrBackground(), ws, hub.AppStudio, 10*time.Second, hub.WithReleaseHandler(a.onReleaseRequested))
		if err != nil {
			_ = ws.Close()
			return nil, takeOverError(err)
		}
	default:
		hold, err = hub.AcquireHub(ws, hub.AppStudio, hub.WithReleaseHandler(a.onReleaseRequested))
		var held *hub.HeldError
		switch {
		case err == nil:
		case errors.As(err, &held):
			readOnly, holder, hold = true, held.Holder, nil
		default:
			_ = ws.Close()
			return nil, fmt.Errorf("failed to take the workspace's database: %w", err)
		}
	}

	// The session shares the app's redactor, so a credential resolved for a
	// connection is scrubbed from its connect and query errors (#114).
	sopts := []workspace.SessionOption{workspace.WithSessionOptions(session.WithRedactor(a.secrets.Redactor()))}
	if readOnly {
		sopts = append(sopts, workspace.WithForceReadOnly())
	}
	sess, issues, err := workspace.OpenSession(a.ctxOrBackground(), ws, a.logger, sopts...)
	if err != nil {
		if hold != nil {
			_ = hold.Release()
		}
		_ = ws.Close()
		return nil, err
	}

	a.ws, a.sess, a.hold = ws, sess, hold
	a.engine = engine.NewManager(sess, engine.WithLogger(a.logger))
	a.issues = issues
	a.recordConnSigs(ws)
	a.readOnly, a.holder = readOnly, holder
	a.sessionID = uuid.New().String()
	a.duplicateOf = workspace.DuplicateIDs(root, ws.ID(), a.knownIDs())
	a.startWatcherLocked()
	a.recordRecent(ws.Root(), ws.ID(), ws.Name())

	info := a.infoLocked()
	a.emitEvent("workspace:opened", info)
	if readOnly {
		a.emitEvent("workspace:locked", holderInfo(holder))
	}
	return &OpenWorkspaceResult{Status: "opened", Path: ws.Root(), Info: info}, nil
}

func takeOverError(err error) error {
	switch {
	case errors.Is(err, hub.ErrNotReleasable):
		return errors.New("The program that has this workspace open can't be asked to let go. Close it there, then try again.")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("The other program didn't let go in time. It is still running the workspace; close it there, then try again.")
	}
	return fmt.Errorf("Couldn't take over the workspace: %w", err)
}

func holderInfo(h *hub.Holder) *HolderInfo {
	if h == nil {
		return nil
	}
	return &HolderInfo{App: h.App, PID: h.PID, ServesMCP: h.MCP != nil && !h.MCP.Pending && h.MCP.URL != "", Text: h.String()}
}

// CloseWorkspace closes the open workspace: MCP off, session closed, hub
// released. The frontend flushes its pending notebook saves first.
func (a *App) CloseWorkspace() error {
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.closeWorkspaceLocked("closed")
	return nil
}

// closeWorkspaceLocked tears the open workspace down in the order the hub
// needs: MCP, watcher, session, then the hub lock last, so the next owner can
// open the DuckDB files the moment the lock is free. A no-op with none open.
// reason rides on "workspace:closed" ("closed", "switch", "released").
func (a *App) closeWorkspaceLocked(reason string) {
	// Previewed bundles were compared with this workspace: forget them.
	a.bundles.clear()
	if a.ws == nil {
		return
	}
	root := a.ws.Root()
	// Credentials held in memory (ask each time, session-only) end with the session.
	a.secrets.ForgetSession()
	if a.wmcp != nil {
		_ = a.wmcp.Stop()
		a.wmcp, a.mcpTool = nil, nil
	}
	if a.watchDone != nil {
		close(a.watchDone)
		a.watchDone = nil
	}
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	if a.sess != nil {
		_ = a.sess.Close()
	}
	if a.hold != nil {
		_ = a.hold.Release()
	}
	_ = a.ws.Close()
	a.ws, a.sess, a.hold, a.engine = nil, nil, nil, nil
	a.issues, a.holder, a.readOnly, a.duplicateOf = nil, nil, false, nil
	a.sessionID = ""
	a.activeNotebookID = ""
	a.emitEvent("workspace:closed", map[string]any{"path": root, "reason": reason})
}

// onReleaseRequested runs when another process (`vibe mcp serve`) asks Studio
// to let go of the workspace. It tells the frontend first so open notebooks are
// saved, then closes the workspace, which releases the hub; the window returns
// to the launcher.
func (a *App) onReleaseRequested() {
	a.mu.RLock()
	root := ""
	var hold *hub.Hold
	if a.ws != nil {
		root, hold = a.ws.Root(), a.hold
	}
	a.mu.RUnlock()
	if root == "" || hold == nil {
		return
	}
	a.emitEvent("workspace:releasing", map[string]any{"path": root})
	time.Sleep(releaseGrace)
	a.mu.Lock()
	defer a.mu.Unlock()
	// The person may have switched workspaces during the grace window: only the
	// hold that was asked to let go is closed, never whatever is open now.
	if a.hold != hold {
		return
	}
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.closeWorkspaceLocked("released")
}

// TakeOverWorkspace asks the process that owns the open (read-only) workspace
// to let go, then reopens it with the database. On failure the workspace comes
// back read-only and the error says why.
func (a *App) TakeOverWorkspace() (*OpenWorkspaceResult, error) {
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if !a.readOnly {
		return &OpenWorkspaceResult{Status: "opened", Path: a.ws.Root(), Info: a.infoLocked()}, nil
	}
	root := a.ws.Root()
	res, err := a.openLocked(root, openTakeOver)
	if err != nil {
		// openLocked closed the read-only workspace before trying: put it back.
		if _, rerr := a.openLocked(root, openReadOnly); rerr != nil {
			a.logger.Warn("failed to reopen the workspace read-only after a failed take-over", "error", rerr)
		}
		return nil, err
	}
	return res, nil
}

// ---- watcher ----

func (a *App) startWatcherLocked() {
	w, err := a.ws.Watch()
	if err != nil {
		a.logger.Warn("workspace file watcher unavailable; external edits will show on the next save", "error", err)
		return
	}
	done := make(chan struct{})
	a.watcher, a.watchDone = w, done
	ws := a.ws
	go func() {
		for {
			select {
			case <-done:
				return
			case ch, ok := <-w.Events():
				if !ok {
					return
				}
				a.onDiskChange(ws, ch)
			}
		}
	}()
}

// onDiskChange reports something other than this window changed a file in the
// workspace: a git pull, an editor, Claude Code. Lists refresh; an open
// notebook, query or skill shows the conflict banner if it had edits.
func (a *App) onDiskChange(ws workspace.Workspace, ch workspace.Change) {
	if ch.Kind == workspace.KindConfig {
		if err := ws.Reload(); err != nil {
			a.logger.Warn("vibe.yaml changed on disk but could not be reloaded", "error", err)
		} else {
			// A host changed by a git pull must not keep using the old
			// registration (and the old credential binding).
			a.syncConnections()
		}
		a.emitEvent("workspace:changed", map[string]any{"kind": string(ch.Kind)})
	}
	if ch.Kind == workspace.KindDotEnv {
		// The file's state moved (an allowed .env is now "changed"); the
		// banner and Workspace settings re-read it. Values stay as loaded.
		a.emitEvent("workspace:changed", map[string]any{"kind": "dotenv"})
		return
	}
	a.emitEvent("workspace:changed-on-disk", map[string]any{
		"kind": string(ch.Kind), "id": ch.ID, "op": string(ch.Op), "path": ch.Path,
	})
	a.emitEvent("data:changed")
}

// ---- info ----

// GetWorkspaceInfo describes the open workspace.
func (a *App) GetWorkspaceInfo() (*WorkspaceInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.infoLocked(), nil
}

// GetSessionID returns the open workspace's session token (see App.sessionID).
func (a *App) GetSessionID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sessionID
}

// IsWorkspaceOpen reports whether a workspace is open.
func (a *App) IsWorkspaceOpen() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ws != nil
}

// infoLocked builds WorkspaceInfo; the caller holds a.mu and has checked a.ws.
func (a *App) infoLocked() *WorkspaceInfo {
	ws := a.ws
	ctx := a.ctxOrBackground()
	cfg := ws.Config()
	local := ws.Local()
	pol := agents.PolicyFrom(ws)
	branch, isGit := gitBranch(ws.Root())

	info := &WorkspaceInfo{
		Root: ws.Root(), ID: ws.ID(), Name: ws.Name(), SessionID: a.sessionID,
		GitBranch: branch, IsGit: isGit,
		DefaultConnection: ws.DefaultConnection(),
		Connections:       a.connectionInfosLocked(),
		Policy:            AgentPolicyInfo{Sandbox: string(pol.Mode), AskBeforeWrites: pol.AskBeforeWrites},
		Team: AgentPolicyInfo{
			Sandbox:         orDefault(cfg.Sandbox, workspace.SandboxFull),
			AskBeforeWrites: cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites,
		},
		TeamSandboxSet: cfg.Sandbox != "",
		Local: AgentPolicyInfo{
			Sandbox:         orDefault(local.Agent.Sandbox, workspace.DefaultSandbox),
			AskBeforeWrites: local.Agent.AskBeforeWrites == nil || *local.Agent.AskBeforeWrites,
		},
		ReadOnly:       a.readOnly,
		Holder:         holderInfo(a.holder),
		Issues:         append([]workspace.SessionIssue{}, a.issues...),
		Warnings:       append([]string{}, ws.Warnings()...),
		DuplicateIDs:   append([]string{}, a.duplicateOf...),
		LocalUntrusted: !ws.LocalTrusted(),
		MCPRunning:     a.wmcp != nil,
	}
	if a.readOnly {
		info.Policy.Sandbox = string(agents.ModeReadOnly)
	}
	if nbs, err := ws.Notebooks().List(ctx); err == nil {
		info.Counts.Notebooks = len(nbs)
	}
	if qs, err := ws.Queries().List(ctx); err == nil {
		info.Counts.Queries = len(qs)
	}
	if sk, err := ws.Skills().List(ctx); err == nil {
		info.Counts.Skills = len(sk)
	}
	if tabs, err := a.sess.ListTables(ctx, "", ""); err == nil {
		info.Counts.Tables = len(tabs)
	}
	return info
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (a *App) connectionInfosLocked() []ConnectionInfo {
	status := map[string]session.Status{}
	for _, s := range a.sess.Connections() {
		status[s.Name] = s
	}
	issue := map[string]string{}
	for _, i := range a.issues {
		issue[i.Connection] = i.Message
	}
	infos := map[string]session.ConnectionInfo{}
	for _, i := range a.sess.ConnectionInfos() {
		infos[i.Name] = i
	}
	out := make([]ConnectionInfo, 0, len(a.ws.Connections()))
	for _, c := range a.ws.Connections() {
		ci := ConnectionInfo{
			Name: c.Name, Type: c.Type, Scope: string(c.Scope), ReadOnly: c.IsReadOnly(),
			Default: c.Default, Status: string(session.StatusNotConnected), SignIn: c.SignIn,
			Remote: connections.IsRemote(c.Type),
		}
		if i, ok := infos[c.Name]; ok {
			ci.Remote, ci.Catalogless = i.Remote, i.Catalogless
		}
		for _, sc := range c.Shadows {
			ci.Shadows = append(ci.Shadows, string(sc))
		}
		if s, ok := status[c.Name]; ok {
			ci.Status, ci.Detail, ci.LockedByPID, ci.ReadOnly = string(s.Kind), s.Message, s.LockedByPID, s.ReadOnly
		}
		if msg, ok := issue[c.Name]; ok {
			// The definition could not even be registered (a missing ${ENV},
			// a path outside the folder, a file in use): say so.
			if _, registered := status[c.Name]; !registered {
				ci.Status, ci.Detail = string(session.StatusError), msg
			} else if ci.Detail == "" {
				ci.Detail = msg
			}
		}
		if ci.Status == string(session.StatusError) && ci.Detail != "" {
			// One plain line for the rail; the raw (redacted) text is in the editor's Test.
			if e := classifyTestError(connections.TestResult{Error: ci.Detail}, c.ConnectionDef); e.Code != "error" {
				ci.Detail = e.Message
			}
		}
		switch ci.Status {
		case string(session.StatusExtensionMissing):
			if ti, err := connections.TypeInfoFor(c.Type); err == nil && len(ti.Extensions) > 0 {
				ci.Extension = ti.Extensions[0]
			}
		case string(session.StatusNeedsCredentials):
			if m := serverChangedRE.FindStringSubmatch(ci.Detail); m != nil {
				ci.ServerChanged = &ServerChange{Old: m[1], New: m[2]}
			}
		}
		out = append(out, ci)
	}
	return out
}

// ListCatalogGroups lists every connection that is attached, with its tables,
// for the Tables view and Home. A connection that is not connected yet is
// listed with no tables and is never dialled by this call.
func (a *App) ListCatalogGroups() ([]CatalogGroup, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	ctx := a.ctxOrBackground()
	groups := []CatalogGroup{}
	for _, c := range a.connectionInfosLocked() {
		g := CatalogGroup{
			Connection: c.Name, Type: c.Type, Status: c.Status, Detail: c.Detail,
			ReadOnly: c.ReadOnly, Default: c.Default, Tables: []CatalogTable{},
		}
		if c.Status == string(session.StatusOK) {
			tabs, err := a.sess.ListTables(ctx, c.Name, "")
			if err != nil {
				g.Status, g.Detail = string(session.StatusError), a.sess.Redact(err.Error())
			}
			for _, t := range tabs {
				kind := "table"
				if strings.Contains(strings.ToUpper(t.Type), "VIEW") {
					kind = "view"
				}
				g.Tables = append(g.Tables, CatalogTable{Name: t.Name, Qualified: t.Ref().String(), Kind: kind})
			}
		}
		groups = append(groups, g)
	}
	// The default connection first, as the sidebar and Home read it.
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Default && !groups[j].Default })
	return groups, nil
}

// ---- create ----

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// workspaceSlug is the folder name for a workspace name.
func workspaceSlug(name string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if s == "" {
		return "workspace"
	}
	return s
}

func resolveNewDir(req CreateWorkspaceRequest) (string, error) {
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		if strings.TrimSpace(req.ParentDir) == "" {
			return "", errors.New("Choose where to put the new workspace.")
		}
		dir = filepath.Join(strings.TrimSpace(req.ParentDir), workspaceSlug(req.Name))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %q: %w", dir, err)
	}
	return abs, nil
}

// CheckWorkspaceDir says where a new workspace called name would go inside
// parentDir, and whether that folder is already taken.
func (a *App) CheckWorkspaceDir(parentDir, name string) (*WorkspaceDirCheck, error) {
	dir, err := resolveNewDir(CreateWorkspaceRequest{ParentDir: parentDir, Name: name})
	if err != nil {
		return nil, err
	}
	chk := &WorkspaceDirCheck{Dir: dir}
	if entries, err := os.ReadDir(dir); err == nil {
		chk.Exists = true
		chk.NotEmpty = len(entries) > 0
	} else if !os.IsNotExist(err) {
		chk.Exists = true
	}
	return chk, nil
}

// CreateWorkspace makes a new workspace (vibe.yaml, notebooks/, queries/,
// skills/, data/, the `local` database, optionally a git repository and a
// starter template's tables and notebook) and opens it. With InPlace it
// initialises a folder that already has files, which is the "Make this folder
// a workspace?" flow.
func (a *App) CreateWorkspace(req CreateWorkspaceRequest) (*OpenWorkspaceResult, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" && !req.InPlace {
		return nil, errors.New("Give the workspace a name.")
	}
	dir, err := resolveNewDir(req)
	if err != nil {
		return nil, err
	}
	var tmpl templates.Template
	if req.Template != "" {
		t, ok := templates.Get(req.Template)
		if !ok {
			return nil, fmt.Errorf("unknown template %q; available templates: %s", req.Template, strings.Join(templates.List(), ", "))
		}
		tmpl = t
	}
	if !req.InPlace {
		if entries, rerr := os.ReadDir(dir); rerr == nil && len(entries) > 0 {
			return nil, fmt.Errorf("There's already a folder called %s in %s. Pick another name or location. Vibe won't put a workspace inside a folder that already has files in it, unless you use Open folder instead.", filepath.Base(dir), filepath.Dir(dir))
		}
	}
	initOpts := []workspace.InitOption{workspace.WithGit(req.Git), workspace.WithInitLogger(a.logger)}
	if name != "" {
		initOpts = append(initOpts, workspace.WithName(name))
	}
	res, err := workspace.Init(dir, initOpts...)
	if err != nil {
		if errors.Is(err, workspace.ErrAlreadyWorkspace) {
			return nil, errors.New("That folder is already a workspace. Use Open folder.")
		}
		return nil, fmt.Errorf("Couldn't create the workspace: %w", err)
	}
	root := res.Workspace.Root()
	_ = res.Workspace.Close()

	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	opened, err := a.openLocked(root, openAuto)
	if err != nil {
		return nil, fmt.Errorf("The workspace was created at %s, but could not be opened: %w", root, err)
	}
	opened.Warnings = append(opened.Warnings, res.Notes...)
	if req.Template != "" {
		if a.readOnly {
			opened.Warnings = append(opened.Warnings, "The workspace's database is in use by another program, so the starter template was not added.")
		} else if err := a.seedTemplateLocked(tmpl); err != nil {
			opened.Warnings = append(opened.Warnings, fmt.Sprintf("The workspace was created, but adding the %q template failed: %s", req.Template, a.sess.Redact(err.Error())))
		} else {
			opened.Info = a.infoLocked()
			a.emitEvent("data:changed")
		}
	}
	return opened, nil
}

// seedTemplateLocked writes a template's tables into the default connection and
// its notebook into notebooks/.
func (a *App) seedTemplateLocked(tmpl templates.Template) error {
	ctx := a.ctxOrBackground()
	for _, i := range a.issues {
		if i.Connection == a.ws.DefaultConnection() {
			return fmt.Errorf("the default connection is not available: %s", i.Message)
		}
	}
	err := templates.SeedTables(ctx, func(ctx context.Context, q string) error {
		_, err := a.sess.Exec(ctx, q)
		return err
	}, tmpl)
	if err != nil {
		return err
	}
	if _, _, err := a.ws.Notebooks().Insert(ctx, tmpl.Book); err != nil {
		return fmt.Errorf("failed to write the starter notebook: %w", err)
	}
	return nil
}

// RegenerateWorkspaceID gives the open workspace a fresh id (a copied folder
// shares its original's id, and so its keychain entries).
func (a *App) RegenerateWorkspaceID() (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if _, err := workspace.RegenerateID(a.ws.Root()); err != nil {
		return nil, fmt.Errorf("failed to give this workspace a new id: %w", err)
	}
	if err := a.ws.Reload(); err != nil {
		return nil, fmt.Errorf("failed to reload the workspace: %w", err)
	}
	a.duplicateOf = nil
	a.recordRecent(a.ws.Root(), a.ws.ID(), a.ws.Name())
	return a.infoLocked(), nil
}

// TrustLocalSettings confirms the saved per-person settings (agent limits you
// loosened, sign-in methods) belong to this folder after it was deleted and
// cloned again.
func (a *App) TrustLocalSettings() (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if err := a.ws.TrustLocal(); err != nil {
		return nil, fmt.Errorf("failed to confirm your settings for this workspace: %w", err)
	}
	if err := a.ws.Reload(); err != nil {
		return nil, fmt.Errorf("failed to reload the workspace: %w", err)
	}
	return a.infoLocked(), nil
}

// ---- settings ----

var tierRank = map[string]int{workspace.SandboxReadOnly: 0, workspace.SandboxReadWrite: 1, workspace.SandboxFull: 2}

// saveLocalAgentLocked saves this person's tier and gate. The tier may not
// exceed the committed team limit, and the gate may not be turned off when
// vibe.yaml requires it. The caller holds a.mu for writing.
func (a *App) saveLocalAgentLocked(sandbox string, ask bool) error {
	mode, err := agents.ParseSandboxMode(sandbox)
	if err != nil {
		return err
	}
	cfg := a.ws.Config()
	if cfg.Sandbox != "" && tierRank[string(mode)] > tierRank[cfg.Sandbox] {
		return fmt.Errorf("The team limit in vibe.yaml is %s, so this computer can't go above it.", cfg.Sandbox)
	}
	if !ask && cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites {
		return errors.New("vibe.yaml requires the agent to ask before changes, so this can't be turned off on this computer.")
	}
	local := a.ws.Local()
	local.Agent.Sandbox = string(mode)
	if ask {
		local.Agent.AskBeforeWrites = nil
	} else {
		local.Agent.AskBeforeWrites = workspace.Bool(false)
	}
	if err := a.ws.SaveLocal(local); err != nil {
		if errors.Is(err, workspace.ErrLocalUntrusted) {
			return errors.New("Your saved settings for this workspace were made for a different folder. Confirm them first.")
		}
		return fmt.Errorf("failed to save your settings: %w", err)
	}
	return nil
}

// SetWorkspaceSettings saves the Settings > Workspace form: the name and default
// connection and the team limit go into vibe.yaml (shared through git), and this
// person's own agent choice goes into the per-person file outside the folder.
// vibe.yaml can only tighten: a person's choice above the team limit is refused.
func (a *App) SetWorkspaceSettings(s WorkspaceSettings) (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	team := s.TeamSandbox
	if team == workspace.SandboxFull {
		team = "" // no limit
	}
	if err := workspace.ValidateSandbox(team); err != nil {
		return nil, err
	}
	if s.DefaultConnection != "" {
		found := false
		for _, c := range a.ws.Connections() {
			if c.Name == s.DefaultConnection {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("There's no connection called %q.", s.DefaultConnection)
		}
	}
	mode, err := agents.ParseSandboxMode(s.Sandbox)
	if err != nil {
		return nil, err
	}
	if team != "" && tierRank[string(mode)] > tierRank[team] {
		return nil, fmt.Errorf("The team limit is %s, so this computer can't go above it.", team)
	}
	if !s.AskBeforeChanges && s.TeamAskBeforeChanges {
		return nil, errors.New("When the team requires the agent to ask before changes, it can't be turned off on this computer.")
	}

	if !a.ws.LocalTrusted() {
		return nil, errors.New("Your saved settings for this workspace were made for a different folder. Confirm them first.")
	}
	cfg := a.ws.Config()
	if s.LoadedTeamSandbox != nil || s.LoadedTeamAsk != nil {
		curSandbox := cfg.Sandbox
		if curSandbox == workspace.SandboxFull {
			curSandbox = ""
		}
		curAsk := cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites
		if (s.LoadedTeamSandbox != nil && *s.LoadedTeamSandbox != curSandbox) || (s.LoadedTeamAsk != nil && *s.LoadedTeamAsk != curAsk) {
			return nil, errors.New(conflictPrefix + "the team's agent limit in vibe.yaml changed since Settings was opened. Reload the page and try again.")
		}
	}
	next := cfg
	next.Name = strings.TrimSpace(s.Name)
	next.DefaultConnection = s.DefaultConnection
	next.Sandbox = team
	next.AskBeforeWrites = nil
	if s.TeamAskBeforeChanges {
		next.AskBeforeWrites = workspace.Bool(true)
	}
	if !configEqual(cfg, next) {
		if err := a.ws.SaveConfig(next); err != nil {
			if errors.Is(err, workspace.ErrConflict) {
				return nil, errors.New(conflictPrefix + "vibe.yaml changed on disk since Settings was opened. Reload and try again.")
			}
			return nil, err
		}
	}
	if err := a.saveLocalAgentLocked(string(mode), s.AskBeforeChanges); err != nil {
		return nil, err
	}
	a.recordRecent(a.ws.Root(), a.ws.ID(), a.ws.Name())
	policy := a.agentPolicy()
	a.emitEvent("agent:config", map[string]any{"sandbox": string(policy.Mode), "askBeforeWrites": policy.AskBeforeWrites})
	a.emitEvent("workspace:changed", map[string]any{"kind": "settings"})
	return a.infoLocked(), nil
}

func configEqual(a, b workspace.Config) bool {
	boolEq := func(x, y *bool) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		return *x == *y
	}
	return a.Name == b.Name && a.DefaultConnection == b.DefaultConnection &&
		a.Sandbox == b.Sandbox && boolEq(a.AskBeforeWrites, b.AskBeforeWrites)
}

// ---- conflicts ----

// ResolveConflict settles a save that was refused because the file changed on
// disk: "reload" keeps the disk version (the frontend then re-reads it),
// "overwrite" writes the local edit over it, and "saveCopy" writes the local edit
// as a new item named "<title> (copy)".
func (a *App) ResolveConflict(req ConflictRequest) (*ConflictResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	ctx := a.ctxOrBackground()
	switch req.Action {
	case "reload", "overwrite", "saveCopy":
	default:
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
	var res *ConflictResult
	var err error
	switch req.Kind {
	case "notebook":
		res, err = a.resolveNotebookConflict(ctx, req)
	case "query":
		res, err = a.resolveQueryConflict(ctx, req)
	case "skill":
		res, err = a.resolveSkillConflict(ctx, req)
	default:
		return nil, fmt.Errorf("unknown kind %q", req.Kind)
	}
	if err != nil {
		return nil, conflictErr(err)
	}
	a.emitEvent("data:changed")
	return res, nil
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func copyTitle(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		t = "Untitled"
	}
	return t + " (copy)"
}

func (a *App) resolveNotebookConflict(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	store := a.ws.Notebooks()
	if req.Action == "reload" {
		nb, _, err := store.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return &ConflictResult{ID: nb.ID, Rev: nb.Rev, Title: nb.Title}, nil
	}
	raw := map[string]any{}
	for k, v := range req.Data {
		raw[k] = v
	}
	var (
		nb  notebooks.Notebook
		err error
	)
	if req.Action == "overwrite" {
		raw["id"] = req.ID
		delete(raw, "rev")
		if req.Rev != "" {
			nb, _, err = store.Save(ctx, raw, notebooks.IfRev(req.Rev))
		} else {
			nb, _, err = store.Save(ctx, raw, notebooks.Overwrite())
		}
	} else {
		delete(raw, "id")
		delete(raw, "rev")
		raw["title"] = copyTitle(str(raw, "title"))
		nb, _, err = store.Save(ctx, raw)
	}
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &ConflictResult{ID: nb.ID, Rev: nb.Rev, Title: nb.Title}, nil
}

func (a *App) resolveQueryConflict(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	store := a.ws.Queries()
	if req.Action == "reload" {
		q, err := store.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return &ConflictResult{ID: q.ID, Rev: q.Rev, Title: q.Title}, nil
	}
	// Keep what the editor does not edit (parameters, description); the connection
	// is the editor's when it sent one.
	q := &queries.Query{Title: str(req.Data, "title"), SQL: str(req.Data, "sql"), Engine: "duckdb", Parameters: []queries.Param{}}
	if base, err := store.Get(ctx, req.ID); err == nil && base != nil {
		q.Connection, q.Description, q.Parameters, q.Engine = base.Connection, base.Description, base.Parameters, base.Engine
	}
	if req.Connection != nil {
		if *req.Connection != "" {
			if err := session.ValidateName(*req.Connection); err != nil {
				return nil, fmt.Errorf("unknown connection %q", *req.Connection)
			}
		}
		q.Connection = *req.Connection
	}
	if d := str(req.Data, "description"); d != "" {
		q.Description = d
	}
	if req.Action == "overwrite" {
		q.ID = req.ID
		var opt queries.WriteOption = queries.Overwrite()
		if req.Rev != "" {
			opt = queries.WithRev(req.Rev) // only the revision the banner was raised against
		}
		if err := store.Update(ctx, q, opt); err != nil {
			if errors.Is(err, queries.ErrNotFound) { // deleted on disk: write it back
				q.ID = ""
				err = store.Create(ctx, q)
			}
			if err != nil {
				return nil, err
			}
		}
	} else {
		q.Title = copyTitle(q.Title)
		if err := store.Create(ctx, q); err != nil {
			return nil, err
		}
	}
	return &ConflictResult{ID: q.ID, Rev: q.Rev, Title: q.Title}, nil
}

func (a *App) resolveSkillConflict(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	store := a.ws.Skills()
	cur, err := store.Get(ctx, req.ID)
	if err != nil && !errors.Is(err, skills.ErrNotFound) {
		return nil, err
	}
	if req.Action == "reload" {
		if cur == nil {
			return nil, skills.ErrNotFound
		}
		return &ConflictResult{ID: cur.Name, Rev: cur.Rev, Title: cur.Name}, nil
	}
	name, rev := req.ID, ""
	var putOpts []skills.PutOption
	if req.Action == "overwrite" {
		switch {
		case cur == nil:
			putOpts = append(putOpts, skills.Overwrite()) // deleted on disk: write it back
		case req.Rev != "":
			rev = req.Rev // only the revision the banner was raised against, as for queries and notebooks
		default:
			rev = cur.Rev // no revision known: replace whatever is on disk now
		}
	} else {
		name = req.ID + "-copy"
		for n := 2; n < 100; n++ { // never overwrite an earlier copy
			if ex, gerr := store.Get(ctx, name); gerr != nil || ex == nil {
				break
			}
			name = fmt.Sprintf("%s-copy-%d", req.ID, n)
		}
	}
	newRev, err := agents.SaveUserSkillAt(store, a.sess, name,
		str(req.Data, "description"), str(req.Data, "instructions"), str(req.Data, "tableName"), rev, putOpts...)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: name, Rev: newRev, Title: name}, nil
}

// ---- MCP (Studio's own switch) ----

// StartMCP serves this workspace's agent tools over MCP on a loopback port and
// publishes the endpoint in hub.lock, so `vibe mcp serve` (what Claude Desktop
// and Claude Code launch) proxies to Studio. A write that needs approval shows
// the same approval card the chat agent uses.
func (a *App) StartMCP() (*MCPStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if a.readOnly || a.hold == nil {
		return nil, errors.New("This window opened the workspace read-only, so it can't serve MCP. Take over the workspace first.")
	}
	if a.wmcp == nil {
		h := a.newAgentToolHandler(agents.ModeFull)
		h.enableApprovals("mcp")
		m, err := hub.StartWorkspaceMCP(a.ws, a.sess, a.hold,
			hub.WithMCPLogger(a.logger),
			hub.WithMCPRememberPort(),
			hub.WithMCPHandler(h.inner),
			hub.WithMCPHandlerWrap(func(vibemcp.Handler) vibemcp.Handler { return h }),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to start the MCP server: %w", err)
		}
		a.wmcp, a.mcpTool = m, h
		a.logger.Info("MCP server started", "url", m.URL())
	}
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return a.mcpStatusLocked(), nil
}

// StopMCP turns Studio's MCP server off and clears the endpoint from hub.lock.
func (a *App) StopMCP() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wmcp == nil {
		return nil
	}
	err := a.wmcp.Stop()
	a.wmcp, a.mcpTool = nil, nil
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return err
}

// GetMCPStatus reports whether Studio's MCP server is running and where. It
// never includes the bearer token.
func (a *App) GetMCPStatus() *MCPStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mcpStatusLocked()
}

func (a *App) mcpStatusLocked() *MCPStatus {
	st := &MCPStatus{CanStart: a.ws != nil && !a.readOnly && a.hold != nil}
	switch {
	case a.ws == nil:
		st.Reason = "No workspace is open."
	case a.readOnly:
		st.Reason = "This window opened the workspace read-only. Take over the workspace to serve MCP."
	}
	if a.wmcp != nil {
		st.Running, st.URL = true, a.wmcp.URL()
	}
	return st
}

// GetClaudeConfig returns the two snippets "Copy Claude Desktop config" and
// "Copy Claude Code config" put on the clipboard. Neither contains a token: they
// launch `vibe mcp serve --workspace DIR`, which reads the endpoint from
// hub.lock when Studio owns the workspace.
func (a *App) GetClaudeConfig() (*hub.ClaudeConfig, error) {
	a.mu.RLock()
	root := ""
	if a.ws != nil {
		root = a.ws.Root()
	}
	a.mu.RUnlock()
	if root == "" {
		return nil, errNoWorkspace
	}
	locate := a.locateVibe
	if locate == nil {
		locate = hub.LocateVibe
	}
	exe, err := locate()
	if err != nil {
		return nil, errors.New("Couldn't find the `vibe` command-line program. Install it, or put it on your PATH, then try again.")
	}
	cfg, err := hub.BuildClaudeConfig(root, exe)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
