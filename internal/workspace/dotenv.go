package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/dotenv"
	"github.com/SandwichLabs/vibe/internal/secrets"
)

// DotEnvDecisionFile is the per-person file, beside local.yaml in the user
// config directory, that remembers allow and deny decisions for the
// workspace's .env. It is never written into the workspace folder.
const DotEnvDecisionFile = "dotenv.json"

// DotEnvSatisfies is one env: sign-in a .env would feed. Names only.
type DotEnvSatisfies struct {
	Connection string   `json:"connection"`
	SignIn     string   `json:"signIn"`
	Variables  []string `json:"variables"`
}

// DotEnvStatus describes the workspace's .env for the UI and CLI. It carries
// variable names, never values.
type DotEnvStatus struct {
	// State is none (no usable file), undecided, allowed, changed (the file
	// differs from what was allowed) or denied.
	State string `json:"state"`
	Path  string `json:"path"`
	// Names are the variables the file sets, sorted.
	Names []string `json:"names"`
	// Warnings are skipped lines (line numbers and names only).
	Warnings []string `json:"warnings,omitempty"`
	// Ignored says why an existing file is not used (symlink out of the
	// workspace, too large).
	Ignored string `json:"ignored,omitempty"`
	// TrackedByGit is true when git tracks the file: a committed credential.
	TrackedByGit bool `json:"trackedByGit"`
	// Loaded is true while values from the file are in the overlay. After an
	// edit the state is "changed" but Loaded stays true until the new content
	// is allowed (or the file is denied or revoked).
	Loaded bool `json:"loaded"`
	// Satisfies lists the env: sign-ins of this workspace's connections whose
	// variables the file sets.
	Satisfies []DotEnvSatisfies `json:"satisfies"`
}

// dotenvState is the workspace's .env overlay.
type dotenvState struct {
	mu        sync.RWMutex
	vars      map[string]string // the approved values; nil when nothing is loaded
	decisions *dotenv.Decisions
	gitSet    bool
	gitKey    string
	gitVal    bool
}

func (w *workspace) initDotEnv(id string) {
	path := ""
	if w.configDir != "" {
		path = filepath.Join(filepath.Dir(w.localPath(id)), DotEnvDecisionFile)
	}
	w.denv.decisions = dotenv.NewDecisions(path)
	w.res = w.opts.resolver.WithEnvLayer(w.overlayGet, w.dotenvHint)
	// Load what an earlier Allow approved, when the file is still the same.
	f := dotenv.Read(w.root)
	if f.Present && w.denv.decisions.State(f.Path, f.Hash) == dotenv.StateAllowed {
		w.setOverlay(f.Vars)
	}
}

func (w *workspace) overlayGet(k string) (string, bool) {
	w.denv.mu.RLock()
	defer w.denv.mu.RUnlock()
	v, ok := w.denv.vars[k]
	return v, ok
}

// setOverlay replaces the loaded values and teaches the redactor every one.
func (w *workspace) setOverlay(vars map[string]string) {
	var cp map[string]string
	if vars != nil {
		cp = make(map[string]string, len(vars))
		for k, v := range vars {
			cp[k] = v
			if v != "" && secretish(k, v) {
				w.opts.resolver.Redactor().Add(v)
			}
		}
	}
	w.denv.mu.Lock()
	w.denv.vars = cp
	w.denv.mu.Unlock()
}

// dotenvHint explains, for an env: sign-in that found nothing, that a .env
// which sets its variables is present but not in use.
func (w *workspace) dotenvHint(prefix string) string {
	f := dotenv.Read(w.root)
	if !f.Present {
		return ""
	}
	sets := false
	for _, n := range f.Names {
		for _, v := range secrets.EnvVarNames(prefix) {
			if n == v {
				sets = true
			}
		}
	}
	if !sets {
		return ""
	}
	switch w.denv.decisions.State(f.Path, f.Hash) {
	case dotenv.StateDenied:
		return "This workspace's .env sets it, but you denied loading it (Workspace settings > .env file, or `vibe workspace env allow`)."
	case dotenv.StateChanged:
		return "This workspace's .env sets it, but the file changed since you allowed it; allow it again."
	case dotenv.StateUndecided:
		return "This workspace's .env sets it; allow loading it (Workspace settings > .env file, or `vibe workspace env allow`)."
	}
	return ""
}

func (w *workspace) DotEnv() DotEnvStatus {
	f := dotenv.Read(w.root)
	st := DotEnvStatus{Path: f.Path, Names: f.Names, Warnings: f.Warnings, Ignored: f.Ignored, Satisfies: []DotEnvSatisfies{}}
	if st.Names == nil {
		st.Names = []string{}
	}
	st.State = string(w.denv.decisions.State(f.Path, f.Hash))
	w.denv.mu.RLock()
	st.Loaded = w.denv.vars != nil
	w.denv.mu.RUnlock()
	if !f.Present {
		return st
	}
	st.TrackedByGit = w.trackedCached(f.Path)
	set := map[string]bool{}
	for _, n := range f.Names {
		set[n] = true
	}
	for _, c := range w.Connections() {
		kind, prefix, err := secrets.ParseMethod(c.SignIn)
		if err != nil || kind != "env" {
			continue
		}
		var vars []string
		for _, v := range secrets.EnvVarNames(prefix) {
			if set[v] {
				vars = append(vars, v)
			}
		}
		if len(vars) > 0 {
			st.Satisfies = append(st.Satisfies, DotEnvSatisfies{Connection: c.Name, SignIn: c.SignIn, Variables: vars})
		}
	}
	sort.Slice(st.Satisfies, func(i, j int) bool { return st.Satisfies[i].Connection < st.Satisfies[j].Connection })
	return st
}

// AllowDotEnv approves the .env as it is now (path plus content hash) and
// loads its values. Any later change to the file ends the approval.
func (w *workspace) AllowDotEnv() error {
	f := dotenv.Read(w.root)
	if !f.Present {
		if f.Ignored != "" {
			return fmt.Errorf("the .env is not used: %s", f.Ignored)
		}
		return fmt.Errorf("this workspace has no .env")
	}
	if err := w.denv.decisions.Allow(f.Path, f.Hash); err != nil {
		return err
	}
	w.setOverlay(f.Vars) // the bytes that were hashed, not a second read
	return nil
}

// DenyDotEnv records that this .env must not be loaded, until revoked, and
// drops any loaded values.
func (w *workspace) DenyDotEnv() error {
	path := filepath.Join(w.root, dotenv.File)
	if err := w.denv.decisions.Deny(path); err != nil {
		return err
	}
	w.setOverlay(nil)
	return nil
}

// RevokeDotEnv forgets the decision (the next open asks again) and drops any
// loaded values.
func (w *workspace) RevokeDotEnv() error {
	if err := w.denv.decisions.Revoke(filepath.Join(w.root, dotenv.File)); err != nil {
		return err
	}
	w.setOverlay(nil)
	return nil
}

// gitTracks reports whether git tracks rel under root. Any failure (no git,
// not a repository) is "no". An untrusted workspace's .git/config must never
// run code: core.fsmonitor and friends are switched off, the system config is
// skipped, and git is only run when a .git exists at all.
func gitTracks(root, rel string) bool {
	if gitDir(root) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false",
		"-C", root, "ls-files", "--error-unmatch", "--", rel)
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1")
	return cmd.Run() == nil
}

// trackedCached is gitTracks cached per .env modification time and .git/index
// time, so a status call does not shell out every time.
func (w *workspace) trackedCached(envPath string) bool {
	key := ""
	if fi, err := os.Stat(envPath); err == nil {
		key = fi.ModTime().String()
	}
	if fi, err := os.Stat(filepath.Join(gitDir(w.root), "index")); err == nil {
		key += "|" + fi.ModTime().String()
	}
	w.denv.mu.Lock()
	defer w.denv.mu.Unlock()
	if w.denv.gitSet && w.denv.gitKey == key {
		return w.denv.gitVal
	}
	v := gitTracks(w.root, dotenv.File)
	w.denv.gitSet, w.denv.gitKey, w.denv.gitVal = true, key, v
	return v
}

// secretish says whether a variable's value is registered with the redactor:
// credential-named variables and any long value. Short plain settings such as
// PORT=5432 would otherwise mask every error that mentions the number.
func secretish(name, v string) bool {
	return len(v) >= 16 || secrets.IsSecretName(name)
}

// gitDir returns the nearest ancestor-or-self directory of root that has a
// .git entry ("" when none). The workspace may sit in a subfolder of a repo.
func gitDir(root string) string {
	d := root
	for {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return filepath.Join(d, ".git")
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}
