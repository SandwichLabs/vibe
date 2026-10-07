package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sandwich-labs/vibe-app/internal/bundle"
	"github.com/sandwich-labs/vibe-app/internal/filestore"
	"github.com/sandwich-labs/vibe-app/internal/llm/skills"
	"github.com/sandwich-labs/vibe-app/internal/workspace"
)

// .vibe bundles (#118). A bundle is something the person exports and imports;
// no agent tool and no MCP method reaches any of this (see
// internal/bundle/isolation_test.go). The passphrase crosses the Wails bridge
// one way only (into Go), is never stored on the App, and is never logged.

// bundleCreatedBy is recorded in a bundle's manifest.
const bundleCreatedBy = "Vibe Studio"

// maxOpenBundles bounds how many previewed bundles are held in memory at once
// (each is at most 50 MB unpacked); the oldest is dropped past that.
const maxOpenBundles = 3

// bundleHandleTTL is how long a preview can wait for its commit.
const bundleHandleTTL = 30 * time.Minute

type heldBundle struct {
	pkg     *bundle.Package
	expires time.Time
}

// bundleStore holds previewed bundles by handle.
type bundleStore struct {
	mu   sync.Mutex
	held map[string]*heldBundle
	// order lists handles oldest first.
	order []string
}

func (s *bundleStore) put(pkg *bundle.Package) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil {
		s.held = map[string]*heldBundle{}
	}
	s.expireLocked()
	for len(s.order) >= maxOpenBundles {
		delete(s.held, s.order[0])
		s.order = s.order[1:]
	}
	h := uuid.NewString()
	s.held[h] = &heldBundle{pkg: pkg, expires: time.Now().Add(bundleHandleTTL)}
	s.order = append(s.order, h)
	return h
}

// has reports whether h still names a held bundle.
func (s *bundleStore) has(h string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	_, ok := s.held[h]
	return ok
}

// take removes and returns a held bundle in one step: of two commits racing for
// one handle, only one gets it.
func (s *bundleStore) take(h string) (*bundle.Package, bool) {
	// Lookup, expiry, removal and the order trim happen under one lock: with the
	// steps separate, two commits could both find the handle and both get it.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	e, ok := s.held[h]
	if !ok {
		return nil, false
	}
	s.dropLocked(h)
	return e.pkg, true
}

// clear forgets every held bundle (the workspace they were previewed against
// was closed or switched).
func (s *bundleStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held, s.order = nil, nil
}

// restore puts a taken bundle back under its handle.
func (s *bundleStore) restore(h string, pkg *bundle.Package) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil {
		s.held = map[string]*heldBundle{}
	}
	s.held[h] = &heldBundle{pkg: pkg, expires: time.Now().Add(bundleHandleTTL)}
	s.order = append(s.order, h)
}

func (s *bundleStore) drop(h string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(h)
}

func (s *bundleStore) dropLocked(h string) {
	delete(s.held, h)
	for i, o := range s.order {
		if o == h {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *bundleStore) expireLocked() {
	now := time.Now()
	for h, e := range s.held {
		if now.After(e.expires) {
			delete(s.held, h)
			for i, o := range s.order {
				if o == h {
					s.order = append(s.order[:i], s.order[i+1:]...)
					break
				}
			}
		}
	}
}

var errBundleGone = errors.New("This import has expired. Choose the file again.")

// ---- what can be exported ----

// BundleContentItem is one notebook, query or skill that can go in a bundle.
type BundleContentItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// BundleContentConnection is one connection definition that could go in a
// bundle. Portable is false (with Reason) for one that cannot leave this
// computer, such as a file path outside the workspace.
type BundleContentConnection struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Summary  string `json:"summary"`
	Scope    string `json:"scope"` // "team" or "this computer"
	Portable bool   `json:"portable"`
	Reason   string `json:"reason,omitempty"`
}

// BundleContent is everything the Export dialog can offer.
type BundleContent struct {
	WorkspaceName string                    `json:"workspaceName"`
	Notebooks     []BundleContentItem       `json:"notebooks"`
	Queries       []BundleContentItem       `json:"queries"`
	Skills        []BundleContentItem       `json:"skills"`
	Connections   []BundleContentConnection `json:"connections"`
}

// GetBundleContent lists what the open workspace could export.
func (a *App) GetBundleContent() (*BundleContent, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	ctx := a.ctxOrBackground()
	out := &BundleContent{WorkspaceName: a.ws.Name(), Notebooks: []BundleContentItem{}, Queries: []BundleContentItem{}, Skills: []BundleContentItem{}, Connections: []BundleContentConnection{}}
	nbs, err := a.ws.Notebooks().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	for _, n := range nbs {
		if n.Error == "" {
			out.Notebooks = append(out.Notebooks, BundleContentItem{ID: n.ID, Title: n.Title})
		}
	}
	qs, err := a.ws.Queries().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}
	for _, q := range qs {
		if q.Error == "" {
			out.Queries = append(out.Queries, BundleContentItem{ID: q.ID, Title: q.Title})
		}
	}
	sks, err := a.ws.Skills().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}
	for _, s := range sks {
		if s.Error == "" && s.Source != skills.SourceBuiltin {
			out.Skills = append(out.Skills, BundleContentItem{ID: s.Name, Title: s.Description})
		}
	}
	add := func(scope string, defs []bundleDef) {
		for _, d := range defs {
			if d.Name == workspace.DefaultConnectionName && d.Path == workspace.DefaultConnectionPath {
				continue // every workspace has its own; a bundle has no data to go with it
			}
			c := BundleContentConnection{Name: d.Name, Type: d.Type, Scope: scope, Portable: true}
			if _, _, err := bundle.SanitizeConnection(d); err != nil {
				c.Portable, c.Reason = false, portableReason(err)
			}
			c.Summary = connectionSummary(d)
			out.Connections = append(out.Connections, c)
		}
	}
	add("team", a.ws.Config().Connections)
	add("this computer", a.ws.Local().Connections)
	sort.SliceStable(out.Connections, func(i, j int) bool { return out.Connections[i].Name < out.Connections[j].Name })
	return out, nil
}

// ---- export ----

// BundleExportRequest is the Export dialog's answer.
type BundleExportRequest struct {
	// Path is the destination; empty asks with a save dialog.
	Path      string           `json:"path"`
	Selection bundle.Selection `json:"selection"`
	// Passphrase, when non-empty, encrypts the bundle.
	Passphrase string `json:"passphrase"`
}

// BundleExportResult says what was written.
type BundleExportResult struct {
	Path      string   `json:"path"`
	Cancelled bool     `json:"cancelled"`
	Encrypted bool     `json:"encrypted"`
	Notebooks int      `json:"notebooks"`
	Queries   int      `json:"queries"`
	Skills    int      `json:"skills"`
	Connected int      `json:"connections"`
	Bytes     int      `json:"bytes"`
	Warnings  []string `json:"warnings"`
}

// chooseBundleSaveFile asks where to write a bundle.
func (a *App) chooseBundleSaveFile(defaultName string) (string, error) {
	if defaultName == "" {
		defaultName = "workspace"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export workspace content",
		DefaultFilename: filestore.Slug(defaultName) + ".vibe",
		Filters:         []runtime.FileFilter{{DisplayName: "Vibe bundle (*.vibe)", Pattern: "*.vibe"}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open the save dialog: %w", err)
	}
	return path, nil
}

// SelectBundleFile asks which bundle to import.
func (a *App) SelectBundleFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import a .vibe bundle",
		Filters: []runtime.FileFilter{{DisplayName: "Vibe bundle (*.vibe)", Pattern: "*.vibe"}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open the file dialog: %w", err)
	}
	return path, nil
}

// ExportBundle writes the selected content of the open workspace to a .vibe
// bundle. It never reads credentials or data.
func (a *App) ExportBundle(req BundleExportRequest) (*BundleExportResult, error) {
	a.mu.RLock()
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	ws := a.ws
	name := ws.Name()
	a.mu.RUnlock()

	path := req.Path
	if path == "" {
		p, err := a.chooseBundleSaveFile(name)
		if err != nil {
			return nil, err
		}
		if p == "" {
			return &BundleExportResult{Cancelled: true, Warnings: []string{}}, nil
		}
		path = p
	}
	if filepath.Ext(path) == "" {
		path += ".vibe"
	}
	res, err := bundle.Export(a.ctxOrBackground(), ws, bundle.ExportOptions{Selection: req.Selection, Passphrase: req.Passphrase, AppVersion: bundleCreatedBy})
	if err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	// The file dialog already asked about replacing; a typed-in path is the
	// person's own choice too.
	if err := os.WriteFile(path, res.Data, 0o644); err != nil { //nolint:gosec // a file meant to be shared
		return nil, fmt.Errorf("failed to write %s: %w", path, err)
	}
	out := &BundleExportResult{Path: path, Encrypted: res.Encrypted, Bytes: len(res.Data), Warnings: append([]string{}, res.Manifest.Warnings...)}
	for _, e := range res.Manifest.Contents {
		switch e.Kind {
		case bundle.KindNotebook:
			out.Notebooks++
		case bundle.KindQuery:
			out.Queries++
		case bundle.KindSkill:
			out.Skills++
		case bundle.KindConnections:
			out.Connected = 1
		}
	}
	return out, nil
}

// ---- import ----

// BundlePreview is what the Import dialog shows. When the bundle is encrypted
// and no passphrase (or a wrong one) was given, NeedsPassphrase is set and the
// rest is empty.
type BundlePreview struct {
	Handle          string `json:"handle"`
	NeedsPassphrase bool   `json:"needsPassphrase"`
	WrongPassphrase bool   `json:"wrongPassphrase"`
	Path            string `json:"path"`
	bundle.Preview
}

// PreviewBundleImport opens a bundle file, verifies it, and reports what is in
// it and which items already exist in the open workspace (none, when no
// workspace is open: the launcher importing into a new one). Nothing is written.
func (a *App) PreviewBundleImport(path, passphrase string) (*BundlePreview, error) {
	data, err := bundle.ReadFile(path)
	if err != nil {
		return nil, bundleError(err)
	}
	info, err := bundle.Peek(data)
	if err != nil {
		return nil, bundleError(err)
	}
	pv := &BundlePreview{Path: path, Preview: bundle.Preview{Items: []bundle.PreviewItem{}, Connections: []bundle.PreviewConnection{}, Warnings: []string{}}}
	if info.Encrypted && passphrase == "" {
		pv.NeedsPassphrase, pv.Encrypted = true, true
		return pv, nil
	}
	pkg, err := bundle.Open(data, passphrase)
	switch {
	case errors.Is(err, bundle.ErrWrongPassphrase):
		pv.NeedsPassphrase, pv.WrongPassphrase, pv.Encrypted = true, true, true
		return pv, nil
	case err != nil:
		return nil, bundleError(err)
	}
	a.mu.RLock()
	var inner bundle.Preview
	if a.ws != nil {
		inner = pkg.Preview(a.ws)
	} else {
		inner = pkg.Preview(nil)
	}
	a.mu.RUnlock()
	pv.Preview = inner
	pv.Handle = a.bundles.put(pkg)
	return pv, nil
}

// bundleError turns a bundle error into the sentence the dialog shows.
func bundleError(err error) error {
	switch {
	case errors.Is(err, bundle.ErrLegacy):
		return errors.New("This is an older .vibe file that holds data. Vibe no longer opens these.")
	case errors.Is(err, bundle.ErrNotBundle):
		return errors.New("That file isn't a Vibe bundle.")
	case errors.Is(err, bundle.ErrTooLarge):
		return errors.New("That bundle is too large to import.")
	case errors.Is(err, bundle.ErrCorrupt):
		return errors.New("That bundle is damaged or has been changed, so it was not imported. " + err.Error())
	}
	return err
}

// BundleDecisions is the Import dialog's answer.
type BundleDecisions struct {
	// OnConflict applies to items without their own entry: rename (default),
	// overwrite or skip.
	OnConflict string `json:"onConflict"`
	// Items maps "notebook/<id>" and so on to rename, overwrite or skip.
	Items map[string]string `json:"items"`
	// Connections names the definitions to add to "This computer".
	Connections []string `json:"connections"`
}

func (d BundleDecisions) options() bundle.ImportOptions {
	opts := bundle.ImportOptions{OnConflict: bundle.Action(d.OnConflict), IncludeConnections: d.Connections}
	if len(d.Items) > 0 {
		opts.Decisions = make(map[string]bundle.Action, len(d.Items))
		for k, v := range d.Items {
			opts.Decisions[k] = bundle.Action(v)
		}
	}
	return opts
}

// CommitBundleImport writes a previewed bundle into the open workspace. The
// handle is spent the moment it is taken (a bundle is committed once), and the
// workspace stays locked against being closed or switched while it is written.
func (a *App) CommitBundleImport(handle string, d BundleDecisions) (*bundle.Result, error) {
	pkg, ok := a.bundles.take(handle)
	if !ok {
		return nil, errBundleGone
	}
	return a.commitPackage(handle, pkg, d)
}

func (a *App) commitPackage(handle string, pkg *bundle.Package, d BundleDecisions) (*bundle.Result, error) {
	a.mu.RLock()
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	res, err := pkg.Import(a.ctxOrBackground(), a.ws, d.options())
	a.mu.RUnlock()
	if err != nil {
		// Nothing was written (bad options): the person can choose again.
		a.bundles.restore(handle, pkg)
		return nil, err
	}
	a.afterBundleImport(res)
	return &res, nil
}

func (a *App) afterBundleImport(res bundle.Result) {
	touchedConns := false
	for _, o := range res.Outcomes {
		if o.Kind == bundle.KindConnection && o.Error == "" && o.Result != "skipped" {
			touchedConns = true
		}
	}
	// The stores' own writes are not reported by the file watcher, so tell the
	// lists to refresh.
	a.emitNotebooksChanged("", nil)
	a.emitEvent("data:changed")
	if touchedConns {
		a.syncConnections()
		a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
	}
}

// ImportBundleIntoNewWorkspace creates a workspace (the launcher flow) and
// imports a previewed bundle into it. The handle is taken first: opening the
// new workspace drops every other held preview.
func (a *App) ImportBundleIntoNewWorkspace(handle string, req CreateWorkspaceRequest, d BundleDecisions) (*BundleImportOpened, error) {
	pkg, ok := a.bundles.take(handle)
	if !ok {
		return nil, errBundleGone
	}
	opened, err := a.CreateWorkspace(req)
	if err != nil {
		a.bundles.restore(handle, pkg)
		return nil, err
	}
	res, err := a.commitPackage(handle, pkg, d)
	if err != nil {
		return nil, fmt.Errorf("The workspace was created, but the import failed: %w", err)
	}
	return &BundleImportOpened{Opened: opened, Result: res}, nil
}

// BundleImportOpened is the result of importing into a new workspace.
type BundleImportOpened struct {
	Opened *OpenWorkspaceResult `json:"opened"`
	Result *bundle.Result       `json:"result"`
}

// DiscardBundleImport forgets a previewed bundle (the dialog was closed).
func (a *App) DiscardBundleImport(handle string) { a.bundles.drop(handle) }

type bundleDef = workspace.ConnectionDef

func connectionSummary(d workspace.ConnectionDef) string {
	var parts []string
	for _, v := range []string{d.Host, d.Database, d.Path, d.Scope} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// portableReason says in a sentence why a definition cannot go in a bundle.
func portableReason(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "absolute"):
		return "It points at a file path on this computer."
	case strings.Contains(msg, "leaves the workspace"):
		return "It points outside the workspace folder."
	case strings.Contains(msg, "${"):
		return "It reads a value from an environment variable."
	}
	return "It can't be shared as it is."
}
