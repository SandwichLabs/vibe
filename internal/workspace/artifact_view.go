package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/trustfile"
)

// ViewDecisionFile is the per-person file, beside dotenv.json in the user
// config directory, that remembers this person's allow and deny decisions for
// custom artifact views (view: script). It is never written into the workspace
// folder, and never travels in a bundle.
const ViewDecisionFile = "views.json"

// PageAskFile remembers, beside views.json, whether this person lets a scripted
// page use its `ask` capability. Allow is keyed like a view's (folder path and
// hash); a deny is keyed by path alone and survives edits.
const PageAskFile = "pageask.json"

// ErrViewChanged means the view's files changed after the person was shown
// them: the allow is refused so that what runs is what was reviewed.
var ErrViewChanged = errors.New("the view changed since it was shown; review it again")

// ErrNoScriptView means the artifact has no usable custom view (a script view
// or an HTML page) to decide on.
var ErrNoScriptView = errors.New("this artifact has no custom view to decide on")

// ErrDecisionNotSaved means the decision could not be recorded. The detail (it
// can hold a path) goes to the debug log.
var ErrDecisionNotSaved = errors.New("your decision could not be saved")

// ArtifactViewStatus is where one artifact's custom view stands for this
// person, with the exact bytes that the hash covers (so what is shown is what
// Allow approves).
type ArtifactViewStatus struct {
	Name string `json:"name"`
	// Kind is the manifest's view kind ("a2ui" or "script"); "" when the
	// artifact could not be read (see Error).
	Kind string `json:"kind"`
	// State is none (not a script view, or unusable), undecided, allowed,
	// changed (the files differ from what was allowed) or denied.
	State string `json:"state"`
	// Hash is the ViewHash of the files as read now ("" when State is none).
	// Allow takes this value back.
	Hash string `json:"hash"`
	// Manifest and Source are the artifact.yaml and view.js text that Hash
	// covers. A page (kind html) has no Source: its files are Files.
	Manifest string `json:"manifest"`
	Source   string `json:"source"`
	// PageKind is "static" or "scripted" for an HTML page (derived from its
	// files), "" otherwise.
	PageKind string `json:"page_kind,omitempty"`
	// Queries are the manifest queries an HTML page uses, and Capabilities
	// what it declares (scripted pages only), for the allow screen.
	Queries      []string `json:"queries,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	// QueryUses are the queries the page can run with the connection each runs
	// on ("" is the workspace's default database), for the allow prompt: every
	// manifest query for a scripted page, the templates' for a static one.
	QueryUses []ArtifactViewQuery `json:"query_uses,omitempty"`
	// Files are an HTML page's files as the hash covers them, text files with
	// their text.
	Files []ArtifactViewFile `json:"files,omitempty"`
	// PageFiles are the same files' bytes, for the host that renders exactly
	// what was hashed. Never crosses a boundary.
	PageFiles []artifacts.PageFile `json:"-"`
	// Error says why the artifact is unusable (State none).
	Error string `json:"error,omitempty"`
}

// ArtifactViewQuery is a manifest query an HTML page uses, with the connection
// it runs on.
type ArtifactViewQuery struct {
	Name       string `json:"name"`
	Connection string `json:"connection,omitempty"`
	// Used is true when the page's templates name the query. A scripted page
	// can run every manifest query with vibe.run, so its list holds them all
	// (Used false for the ones only script could reach); a static page has no
	// script, so its list holds only the template's.
	Used bool `json:"used"`
}

// ArtifactViewFile is one file of an HTML page, for the allow screen: its
// path under html/, its role in the hash, its size, and its text when it is
// text (a binary asset has none).
type ArtifactViewFile struct {
	Path string `json:"path"`
	Role string `json:"role"`
	Size int    `json:"size"`
	Text string `json:"text,omitempty"`
}

// ArtifactViewDecision is one stored decision, for Settings.
type ArtifactViewDecision struct {
	Name string `json:"name"`
	// Path is the artifact folder the decision is keyed by.
	Path string `json:"path"`
	// Decision is "allow" or "deny".
	Decision string `json:"decision"`
	// State is the current state of that decision: allowed, changed, denied, or
	// gone (the artifact is not in this workspace any more).
	State string `json:"state"`
	// DeclaresAsk is true for a scripted page that declares `ask`; AskOn says
	// whether this person lets it (its own toggle, separate from the allow).
	DeclaresAsk bool `json:"declaresAsk,omitempty"`
	AskOn       bool `json:"askOn,omitempty"`
	// Kind is the view kind ("script" or "html"), PageKind a page's derived
	// host ("static" or "scripted"), Capabilities what a scripted page
	// declares; all empty for a decision whose artifact is gone.
	Kind         string   `json:"kind,omitempty"`
	PageKind     string   `json:"pageKind,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Decision states beyond trustfile's.
const (
	ViewStateNone = "none"
	ViewStateGone = "gone"
)

func (w *workspace) initArtifactViews(id string) {
	path, askPath := "", ""
	if w.configDir != "" {
		dir := filepath.Dir(w.localPath(id))
		path, askPath = filepath.Join(dir, ViewDecisionFile), filepath.Join(dir, PageAskFile)
	}
	w.vdec = trustfile.New(path)
	w.adec = trustfile.New(askPath)
}

// viewKey is the decision key of an artifact: the canonical absolute path of
// its folder. The workspace root is already symlink-resolved; the artifacts/
// folder is resolved again here (a link there is refused by the store, but the
// key must still be the real place), and on case-insensitive platforms the
// path is spelled as the file system spells it, so one folder has one key. The
// name is a filestore ID (lowercase), so the last element needs no care, and
// two different folders never share a key.
func (w *workspace) viewKey(name string) string {
	dir := filepath.Join(w.root, artifacts.Dir)
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	if caseInsensitiveFS() {
		dir = onDiskCase(dir)
	}
	return filepath.Join(dir, name)
}

func caseInsensitiveFS() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

// onDiskCase returns p with each existing element spelled as its parent folder
// lists it. An element that is not found keeps its spelling.
func onDiskCase(p string) string {
	p = filepath.Clean(p)
	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	cur := vol + string(filepath.Separator)
	for _, el := range strings.Split(strings.Trim(rest, string(filepath.Separator)), string(filepath.Separator)) {
		if el == "" {
			continue
		}
		cur = filepath.Join(cur, matchCase(cur, el))
	}
	return cur
}

// matchCase is the entry of dir that equals name without regard to case
// (name itself when there is none or dir cannot be read).
func matchCase(dir, name string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return name
	}
	for _, e := range ents {
		if e.Name() == name {
			return name
		}
	}
	for _, e := range ents {
		if strings.EqualFold(e.Name(), name) {
			return e.Name()
		}
	}
	return name
}

// Fixed texts for what crosses the bridge: an error from the store can carry
// an absolute path, so the detail goes to the debug log only.
const (
	viewErrNotFound = "there is no such artifact"
	viewErrUnusable = "this artifact's files are not usable as a custom view (open its Source tab in Studio, or run vibe artifact list, to see why)"
)

// unusable marks the status as not usable, with a fixed text.
func (w *workspace) unusable(st ArtifactViewStatus, name string, err error) ArtifactViewStatus {
	w.logger.Debug("artifact view unusable", "artifact", name, "error", err)
	st.Error = viewErrUnusable
	st.Hash = ""
	return st
}

// readView reads the artifact's files once and derives kind, hash and text.
func (w *workspace) readView(ctx context.Context, name string) ArtifactViewStatus {
	st := ArtifactViewStatus{Name: name, State: ViewStateNone}
	src, err := w.Artifacts().Source(ctx, name)
	if errors.Is(err, artifacts.ErrNotFound) {
		st.Error = viewErrNotFound
		return st
	}
	if err != nil {
		return w.unusable(st, name, err)
	}
	st.Manifest, st.Source = src.Manifest, src.View
	m, err := artifacts.Decode([]byte(src.Manifest))
	if err != nil {
		return w.unusable(st, name, err)
	}
	st.Kind = m.View
	if m.View == artifacts.ViewHTML {
		return w.readPage(st, name, src)
	}
	if m.View != artifacts.ViewScript {
		return st
	}
	if src.ViewFile != artifacts.ScriptFile || src.ViewMissing {
		return w.unusable(st, name, errors.New("view.js is missing"))
	}
	if err := artifacts.ValidateView(m, []byte(src.View)); err != nil {
		return w.unusable(st, name, err)
	}
	h, err := artifacts.ViewHash([]byte(src.Manifest), []artifacts.ViewPart{{Path: artifacts.ScriptFile, Data: []byte(src.View)}})
	if err != nil {
		return w.unusable(st, name, err)
	}
	st.Hash = h
	return st
}

// readPage derives an HTML page's hash and file list from the one read of its
// files: the same function the host that renders it uses, over the same bytes.
func (w *workspace) readPage(st ArtifactViewStatus, name string, src *artifacts.Source) ArtifactViewStatus {
	a, err := artifacts.PageFromText(name, []byte(src.Manifest), src.Files)
	if err != nil {
		return w.unusable(st, name, err)
	}
	st.Hash, st.PageKind = a.Page.Hash, a.Page.Kind
	st.Queries, st.Capabilities = a.Page.Queries, a.Page.Capabilities
	st.QueryUses = pageQueryUses(a)
	st.PageFiles = a.Files
	st.Source = ""
	for _, f := range a.Files {
		vf := ArtifactViewFile{Path: f.Path, Role: artifacts.PageFileRole(f.Path, a.Page.Entry), Size: len(f.Data)}
		if artifacts.IsTextPageFile(f.Path) && utf8.Valid(f.Data) {
			vf.Text = string(f.Data)
		}
		st.Files = append(st.Files, vf)
	}
	return st
}

// pageQueryUses is the queries the allow prompt lists: for a scripted page every
// manifest query (vibe.run reaches any of them), marked when the templates use
// it; for a static page the queries its templates use.
func pageQueryUses(a *artifacts.Artifact) []ArtifactViewQuery {
	used := map[string]bool{}
	for _, q := range a.Page.Queries {
		used[q] = true
	}
	var out []ArtifactViewQuery
	for _, mq := range a.Manifest.Queries {
		if used[mq.Name] || a.Page.Kind == artifacts.PageScripted {
			out = append(out, ArtifactViewQuery{Name: mq.Name, Connection: mq.Connection, Used: used[mq.Name]})
		}
	}
	return out
}

// customKind reports whether a manifest view kind is one the person decides on.
func customKind(kind string) bool { return kind == artifacts.ViewScript || kind == artifacts.ViewHTML }

// ArtifactViewState reports where the artifact's custom view stands.
func (w *workspace) ArtifactViewState(name string) (ArtifactViewStatus, error) {
	if !filestore.ValidID(name) {
		return ArtifactViewStatus{}, fmt.Errorf("artifact %q: %w", name, artifacts.ErrNotFound)
	}
	st := w.readView(context.Background(), name)
	if st.Hash != "" {
		st.State = string(w.vdec.State(w.viewKey(name), st.Hash))
	}
	return st, nil
}

// AllowArtifactView approves the view whose ViewHash the person was shown. It
// is refused with ErrViewChanged when the files differ now, checked while the
// decision store is locked, so the allow is for the bytes that were reviewed.
func (w *workspace) AllowArtifactView(name, hash string) error {
	if !filestore.ValidID(name) {
		return fmt.Errorf("artifact %q: %w", name, artifacts.ErrNotFound)
	}
	err := w.vdec.AllowIf(w.viewKey(name), hash, func() (string, error) {
		st := w.readView(context.Background(), name)
		if st.Hash == "" {
			return "", ErrNoScriptView
		}
		return st.Hash, nil
	})
	if err == nil {
		w.defaultAskOn(name, hash)
	}
	return w.decisionErr(err, name)
}

// defaultAskOn turns the `ask` toggle on for a scripted page that declares it,
// as the page is allowed (H5: on by default, revocable separately). A person's
// earlier "off" (a deny, which survives edits) is left alone.
func (w *workspace) defaultAskOn(name, hash string) {
	st := w.readView(context.Background(), name)
	if st.Hash != hash || !declaresAsk(st) {
		return
	}
	key := w.viewKey(name)
	if w.adec.State(key, hash) == trustfile.StateDenied {
		return
	}
	if err := w.adec.Allow(key, hash); err != nil {
		w.logger.Debug("page ask toggle not saved", "artifact", name, "error", err)
	}
}

func declaresAsk(st ArtifactViewStatus) bool {
	if st.Kind != artifacts.ViewHTML || st.PageKind != artifacts.PageScripted {
		return false
	}
	for _, c := range st.Capabilities {
		if c == artifacts.CapAsk {
			return true
		}
	}
	return false
}

// PageAskEnabled reports whether the person lets the page, at exactly this
// hash, use `ask`.
func (w *workspace) PageAskEnabled(name, hash string) bool {
	if !filestore.ValidID(name) || hash == "" {
		return false
	}
	return w.adec.State(w.viewKey(name), hash) == trustfile.StateAllowed
}

// SetPageAsk turns the page's `ask` toggle on (for the page as it is now) or
// off (until turned on again, whatever edits follow).
func (w *workspace) SetPageAsk(name string, on bool) error {
	st, err := w.ArtifactViewState(name)
	if err != nil {
		return err
	}
	if !declaresAsk(st) {
		return ErrNoScriptView
	}
	key := w.viewKey(name)
	if on {
		return w.decisionErr(w.adec.Allow(key, st.Hash), name)
	}
	return w.decisionErr(w.adec.Deny(key), name)
}

// decisionErr maps a trustfile or read error to one of the fixed errors.
func (w *workspace) decisionErr(err error, name string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, trustfile.ErrStale):
		return ErrViewChanged
	case errors.Is(err, ErrNoScriptView):
		return ErrNoScriptView
	}
	w.logger.Debug("artifact view decision failed", "artifact", name, "error", err)
	return ErrDecisionNotSaved
}

// DenyArtifactView records that the artifact's view must not run, until
// revoked; edits do not undo it.
func (w *workspace) DenyArtifactView(name string) error {
	st, err := w.ArtifactViewState(name)
	if err != nil {
		return err
	}
	if !customKind(st.Kind) {
		return ErrNoScriptView
	}
	return w.decisionErr(w.vdec.Deny(w.viewKey(name)), name)
}

// RevokeArtifactView forgets any decision about the artifact's view (the next
// open asks again). It works for an artifact that no longer exists.
func (w *workspace) RevokeArtifactView(name string) error {
	if !filestore.ValidID(name) {
		return fmt.Errorf("artifact %q: %w", name, artifacts.ErrNotFound)
	}
	return w.decisionErr(w.vdec.Revoke(w.viewKey(name)), name)
}

// ArtifactViewDecisions lists the stored decisions that belong to this
// workspace's artifacts folder, ordered by name.
func (w *workspace) ArtifactViewDecisions() []ArtifactViewDecision {
	out := []ArtifactViewDecision{}
	for _, e := range w.vdec.List() {
		name := filepath.Base(e.Path)
		if !filestore.ValidID(name) || w.viewKey(name) != e.Path {
			continue // another workspace's, or not keyed the way this one keys
		}
		d := ArtifactViewDecision{Name: name, Path: e.Path, Decision: e.Decision}
		st, _ := w.ArtifactViewState(name)
		d.DeclaresAsk = declaresAsk(st)
		if customKind(st.Kind) {
			d.Kind, d.PageKind, d.Capabilities = st.Kind, st.PageKind, st.Capabilities
		}
		d.AskOn = d.DeclaresAsk && w.PageAskEnabled(name, st.Hash)
		switch {
		case !customKind(st.Kind):
			d.State = ViewStateGone
		case e.Decision == "deny":
			d.State = string(trustfile.StateDenied)
		default:
			d.State = st.State
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
