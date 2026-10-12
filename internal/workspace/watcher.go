package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/SandwichLabs/vibe/internal/dotenv"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// ChangeKind is what changed on disk.
type ChangeKind string

// ChangeOp is how it changed.
type ChangeOp string

// Change kinds and ops.
const (
	KindNotebook ChangeKind = "notebook"
	KindQuery    ChangeKind = "query"
	KindSkill    ChangeKind = "skill"
	KindConfig   ChangeKind = "config"
	// KindDotEnv is the workspace's .env. The change carries no content; ask
	// Workspace.DotEnv for the new state (an allowed file becomes "changed").
	KindDotEnv ChangeKind = "dotenv"

	OpCreated ChangeOp = "created"
	OpChanged ChangeOp = "changed"
	OpDeleted ChangeOp = "deleted"
)

// dotenvFile is the workspace's dotenv file name.
const dotenvFile = dotenv.File

// Change is one debounced, de-duplicated change to a workspace file made by
// something other than this process's own stores: an editor, a git pull,
// Claude Code, another Vibe.
type Change struct {
	Kind ChangeKind `json:"kind"`
	Op   ChangeOp   `json:"op"`
	// ID is the notebook id, query id, skill name, or for config the file
	// name ("vibe.yaml"). Your own per-person settings live outside the
	// folder and are not watched; SaveLocal and Reload cover them.
	ID string `json:"id"`
	// Path is slash-separated and relative to the workspace root.
	Path string `json:"path"`
}

// WatchOption configures Watch.
type WatchOption func(*watchOpts)

type watchOpts struct {
	debounce time.Duration
}

// WithDebounce sets how long the watcher waits for a burst of events to
// settle before reporting (default 150ms).
func WithDebounce(d time.Duration) WatchOption { return func(o *watchOpts) { o.debounce = d } }

// Watcher reports external changes under a workspace. fsnotify is not
// recursive on Linux, so every directory that can hold a watched file is
// registered on its own: the root (for vibe.yaml and the content directories
// appearing later), notebooks/, queries/, skills/, each skills/<name>/ and
// .vibe-local/, and directories created afterwards are added as they appear.
//
// Its own stores' writes are ignored by content hash: a store announces the
// hash of what it is about to write, and the watcher drops any event whose
// file already has that hash. A real external edit changes the hash and is
// reported; so is an external edit that happens to restore the previous
// bytes, once.
type Watcher struct {
	root     string
	fs       *fsnotify.Watcher
	events   chan Change
	done     chan struct{}
	wg       sync.WaitGroup
	logger   *slog.Logger
	debounce time.Duration

	mu      sync.Mutex
	known   map[string]string // abs path -> content hash last seen or written
	watched map[string]bool
	pending map[string]struct{}
	closed  bool
}

// Watch starts a watcher for the workspace and registers it so the
// workspace's stores report their own writes to it.
//
// The state of the files at the moment Watch returns is the baseline: a file
// changed between Watch's directory registration and its snapshot is part of
// the baseline and not reported. Load notebooks, queries and skills after
// Watch returns (or Reload) so nothing loaded earlier is assumed current.
func (w *workspace) Watch(opts ...WatchOption) (*Watcher, error) {
	o := watchOpts{debounce: 150 * time.Millisecond}
	for _, f := range opts {
		f(&o)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to start file watcher: %w", err)
	}
	wt := &Watcher{
		root: w.root, fs: fw, events: make(chan Change, 256), done: make(chan struct{}),
		logger: w.logger, debounce: o.debounce,
		known: map[string]string{}, watched: map[string]bool{}, pending: map[string]struct{}{},
	}
	if err := wt.addTree(); err != nil {
		_ = fw.Close()
		return nil, err
	}
	wt.snapshot()

	w.wmu.Lock()
	w.watchers = append(w.watchers, wt)
	w.wmu.Unlock()

	wt.wg.Add(1)
	go wt.loop()
	return wt, nil
}

// Events delivers changes; it is closed when the watcher is.
func (w *Watcher) Events() <-chan Change { return w.events }

// Close stops the watcher and closes Events.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	close(w.done)
	err := w.fs.Close()
	w.wg.Wait()
	close(w.events)
	return err
}

// noteWrite is called by the stores before they replace or delete a file.
func (w *Watcher) noteWrite(path, hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if hash == "" {
		delete(w.known, path)
		return
	}
	w.known[path] = hash
}

func (w *Watcher) addDir(abs string) bool {
	w.mu.Lock()
	if w.watched[abs] {
		w.mu.Unlock()
		return false
	}
	w.mu.Unlock()
	if err := w.fs.Add(abs); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.logger.Warn("failed to watch directory", "dir", abs, "error", err)
		}
		return false
	}
	w.mu.Lock()
	w.watched[abs] = true
	w.mu.Unlock()
	return true
}

// addTree registers every directory that exists now.
func (w *Watcher) addTree() error {
	if !w.addDir(w.root) {
		return fmt.Errorf("failed to watch workspace %s", w.root)
	}
	for _, d := range []string{notebooks.Dir, queries.Dir, skills.Dir} {
		w.addDir(filepath.Join(w.root, d))
	}
	if subs, err := filestore.ListSubdirs(w.root, skills.Dir); err == nil {
		for _, s := range subs {
			w.addDir(filepath.Join(w.root, skills.Dir, s))
		}
	}
	return nil
}

// relevant lists the abs paths of every watched file that exists now.
func (w *Watcher) relevant() []string {
	var out []string
	add := func(rel string) { out = append(out, filepath.Join(w.root, filepath.FromSlash(rel))) }
	add(ConfigFile)
	add(dotenvFile)
	if names, err := filestore.ListDir(w.root, notebooks.Dir, notebooks.Extension); err == nil {
		for _, n := range names {
			add(notebooks.Dir + "/" + n)
		}
	}
	if names, err := filestore.ListDir(w.root, queries.Dir, queries.Extension); err == nil {
		for _, n := range names {
			add(queries.Dir + "/" + n)
		}
	}
	if subs, err := filestore.ListSubdirs(w.root, skills.Dir); err == nil {
		for _, s := range subs {
			add(skills.Dir + "/" + s + "/" + skills.File)
		}
	}
	return out
}

// snapshot records the hash of every file that exists now, so an event for an
// untouched file is not reported as a change.
func (w *Watcher) snapshot() {
	for _, p := range w.relevant() {
		if h, ok := w.hashOf(p); ok {
			w.mu.Lock()
			if _, seen := w.known[p]; !seen {
				w.known[p] = h
			}
			w.mu.Unlock()
		}
	}
}

// hashOf hashes a regular file that resolves inside the workspace.
func (w *Watcher) hashOf(abs string) (string, bool) {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", false
	}
	data, _, err := filestore.Read(w.root, filepath.ToSlash(rel))
	if err != nil {
		return "", false
	}
	return filestore.HashBytes(data), true
}

// classify maps a workspace-relative slash path to a change kind and id.
func classify(rel string) (ChangeKind, string, bool) {
	parts := strings.Split(rel, "/")
	switch {
	case rel == ConfigFile:
		return KindConfig, ConfigFile, true
	case rel == dotenvFile:
		return KindDotEnv, dotenvFile, true
	case len(parts) == 2 && parts[0] == notebooks.Dir && strings.HasSuffix(parts[1], notebooks.Extension) && !filestore.IsTemp(parts[1]):
		return KindNotebook, strings.TrimSuffix(parts[1], notebooks.Extension), true
	case len(parts) == 2 && parts[0] == queries.Dir && strings.HasSuffix(parts[1], queries.Extension) && !filestore.IsTemp(parts[1]):
		return KindQuery, strings.TrimSuffix(parts[1], queries.Extension), true
	case len(parts) == 3 && parts[0] == skills.Dir && parts[2] == skills.File && !filestore.IsTemp(parts[1]):
		return KindSkill, parts[1], true
	}
	return "", "", false
}

func (w *Watcher) rel(abs string) (string, bool) {
	if !filestore.Within(w.root, abs) {
		return "", false
	}
	r, err := filepath.Rel(w.root, abs)
	if err != nil || r == "." {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			return
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			w.logger.Warn("file watcher error", "error", err)
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if w.handle(ev) {
				timer = rearm(timer, w.debounce)
				timerC = timer.C
			}
		case <-timerC:
			timerC = nil
			w.flush()
		}
	}
}

// rearm starts the debounce timer, or restarts it (draining a fired tick).
func rearm(timer *time.Timer, d time.Duration) *time.Timer {
	if timer == nil {
		return time.NewTimer(d)
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
	return timer
}

// handle queues the event's path and registers newly created directories.
// It reports whether something was queued.
func (w *Watcher) handle(ev fsnotify.Event) bool {
	rel, ok := w.rel(ev.Name)
	if !ok {
		return false
	}
	queued := false
	if (ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)) && w.dropDir(ev.Name) {
		queued = true
	}
	if ev.Has(fsnotify.Create) {
		if fi, err := os.Lstat(ev.Name); err == nil && fi.IsDir() {
			return w.handleNewDir(ev.Name, rel) || queued
		}
	}
	if _, _, ok := classify(rel); !ok {
		return queued
	}
	w.queue(ev.Name)
	return true
}

// handleNewDir registers a directory that appeared, when it is one of the
// watched levels, and queues the files already inside it. It reports whether
// anything was queued.
func (w *Watcher) handleNewDir(abs, rel string) bool {
	parts := strings.Split(rel, "/")
	isWatchedDir := (len(parts) == 1 && (parts[0] == notebooks.Dir || parts[0] == queries.Dir || parts[0] == skills.Dir)) ||
		(len(parts) == 2 && parts[0] == skills.Dir && !filestore.IsTemp(parts[1]))
	if !isWatchedDir || !w.addDir(abs) {
		return false
	}
	// Directories and files may already be inside by the time the
	// watch exists (mkdir -p, a git checkout): register the
	// skills/<name>/ levels and report what is there.
	if len(parts) == 1 && parts[0] == skills.Dir {
		if subs, err := filestore.ListSubdirs(w.root, skills.Dir); err == nil {
			for _, s := range subs {
				w.addDir(filepath.Join(abs, s))
			}
		}
	}
	queued := false
	for _, p := range w.relevant() {
		if strings.HasPrefix(p, abs+string(filepath.Separator)) {
			w.queue(p)
			queued = true
		}
	}
	return queued
}

// dropDir forgets a watched directory that was removed or renamed away (a git
// checkout of another branch, rm -r): the kernel has already dropped its
// watch, and without forgetting it here a recreated directory of the same name
// would be skipped as "already watched". Files known under it are queued so
// their disappearance is reported. It reports whether anything was queued.
func (w *Watcher) dropDir(abs string) bool {
	w.mu.Lock()
	if !w.watched[abs] || abs == w.root {
		w.mu.Unlock()
		return false
	}
	delete(w.watched, abs)
	prefix := abs + string(filepath.Separator)
	var gone []string
	for p := range w.known {
		if strings.HasPrefix(p, prefix) {
			gone = append(gone, p)
		}
	}
	for _, p := range gone {
		w.pending[p] = struct{}{}
	}
	w.mu.Unlock()
	_ = w.fs.Remove(abs) // fails harmlessly when the kernel already dropped it
	return len(gone) > 0
}

func (w *Watcher) queue(abs string) {
	w.mu.Lock()
	w.pending[abs] = struct{}{}
	w.mu.Unlock()
}

// flush turns the settled paths into Changes.
func (w *Watcher) flush() {
	w.mu.Lock()
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	w.pending = map[string]struct{}{}
	w.mu.Unlock()

	for _, abs := range paths {
		rel, ok := w.rel(abs)
		if !ok {
			continue
		}
		kind, id, ok := classify(rel)
		if !ok {
			continue
		}
		hash, exists := w.hashOf(abs)

		w.mu.Lock()
		prev, wasKnown := w.known[abs]
		var op ChangeOp
		switch {
		case exists && !wasKnown:
			op = OpCreated
		case exists && hash != prev:
			op = OpChanged
		case !exists && wasKnown:
			op = OpDeleted
		}
		if exists {
			w.known[abs] = hash
		} else {
			delete(w.known, abs)
		}
		w.mu.Unlock()
		if op == "" {
			continue
		}
		select {
		case w.events <- Change{Kind: kind, Op: op, ID: id, Path: rel}:
		case <-w.done:
			return
		}
	}
}

// WaitForChange is a small helper for callers and tests: it returns the next
// change or ctx's error.
func (w *Watcher) WaitForChange(ctx context.Context) (Change, error) {
	select {
	case c, ok := <-w.events:
		if !ok {
			return Change{}, errors.New("watcher closed")
		}
		return c, nil
	case <-ctx.Done():
		return Change{}, ctx.Err()
	}
}
