package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/skills"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// KindConnection is one definition from connections.yaml.
const KindConnection ItemKind = "connection"

// Action is what to do with one item.
type Action string

// Actions. ActionImport is the default for an item that does not collide with
// anything; for one that does, OnConflict (default rename) applies.
const (
	ActionImport    Action = "import"
	ActionRename    Action = "rename"
	ActionOverwrite Action = "overwrite"
	ActionSkip      Action = "skip"
)

func (a Action) valid() bool {
	switch a {
	case ActionImport, ActionRename, ActionOverwrite, ActionSkip:
		return true
	}
	return false
}

// ImportOptions chooses what happens to each item.
type ImportOptions struct {
	// OnConflict applies to every colliding item without its own decision.
	// Empty means rename.
	OnConflict Action
	// Decisions are per-item choices, keyed by Key(kind, id) (and
	// Key(KindConnection, name)): rename, overwrite or skip for a collision,
	// skip to leave out an item that has none.
	Decisions map[string]Action
	// IncludeConnections names the definitions to add to "This computer".
	// Definitions are never imported unless named here.
	IncludeConnections []string
	// DryRun computes the outcome and writes nothing.
	DryRun bool
}

// PreviewItem is one notebook, query or skill in a bundle.
type PreviewItem struct {
	Key      string   `json:"key"`
	Kind     ItemKind `json:"kind"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Size     int64    `json:"size"`
	Conflict bool     `json:"conflict"`
}

// PreviewConnection is one connection definition in a bundle.
type PreviewConnection struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Summary is host/database or path or prefix, never a secret.
	Summary string `json:"summary"`
	// Details is everything the definition sets (host, database, path,
	// region, storage scope, every option), so nothing that decides where it
	// connects is hidden.
	Details []string `json:"details"`
	// Risks are reasons to look twice (TLS off, a non-AWS endpoint, a whole
	// bucket). AskedWritable is true when the bundle asked for write access;
	// it is imported read-only either way.
	Risks         []string `json:"risks"`
	AskedWritable bool     `json:"askedWritable"`
	// Conflict is true when this workspace already has a connection of that
	// name; ConflictScope says which scope ("team", "this computer", ...) and
	// RenamedTo the name a rename would give it.
	Conflict      bool   `json:"conflict"`
	ConflictScope string `json:"conflictScope,omitempty"`
	RenamedTo     string `json:"renamedTo,omitempty"`
}

// Preview is what a bundle holds, against a workspace.
type Preview struct {
	WorkspaceName string              `json:"workspaceName"`
	CreatedAt     string              `json:"createdAt"`
	CreatedBy     string              `json:"createdBy"`
	Encrypted     bool                `json:"encrypted"`
	Items         []PreviewItem       `json:"items"`
	Connections   []PreviewConnection `json:"connections"`
	// Warnings are Vibe's own; MakerNotes are the manifest's, written by
	// whoever made the bundle.
	Warnings   []string `json:"warnings"`
	MakerNotes []string `json:"makerNotes"`
}

// Outcome is what happened to one item.
type Outcome struct {
	Key     string   `json:"key"`
	Kind    ItemKind `json:"kind"`
	ID      string   `json:"id"`
	FinalID string   `json:"finalId,omitempty"`
	// Result is created, renamed, overwritten or skipped.
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Result is the outcome of Import.
type Result struct {
	Outcomes []Outcome `json:"outcomes"`
	DryRun   bool      `json:"dryRun"`
	Warnings []string  `json:"warnings"`
}

// Failed counts outcomes that carry an error.
func (r Result) Failed() int {
	n := 0
	for _, o := range r.Outcomes {
		if o.Error != "" {
			n++
		}
	}
	return n
}

// noteRev records the revision of the item that is in the workspace now, so an
// overwrite chosen on the preview only replaces that version.
func (p *Package) noteRev(ctx context.Context, t Target, kind ItemKind, id string) {
	var rev string
	switch kind {
	case KindNotebook:
		if nb, _, err := t.Notebooks().Get(ctx, id); err == nil {
			rev = nb.Rev
		}
	case KindQuery:
		if q, err := t.Queries().Get(ctx, id); err == nil {
			rev = q.Rev
		}
	case KindSkill:
		if sk, err := t.Skills().Get(ctx, id); err == nil {
			rev = sk.Rev
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.revs == nil {
		p.revs = map[string]string{}
	}
	if rev != "" {
		p.revs[Key(kind, id)] = rev
	} else {
		delete(p.revs, Key(kind, id))
	}
}

func (p *Package) revFor(ctx context.Context, t Target, kind ItemKind, id string) string {
	p.mu.Lock()
	rev, ok := p.revs[Key(kind, id)]
	p.mu.Unlock()
	if ok {
		return rev
	}
	// No preview of this item (the CLI, a dry run): read it now.
	p.noteRev(ctx, t, kind, id)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revs[Key(kind, id)]
}

// Items lists what the package holds, for a caller that wants ids.
func (p *Package) Items() []PreviewItem {
	out := make([]PreviewItem, 0, len(p.items))
	for _, it := range p.items {
		out = append(out, PreviewItem{Key: Key(it.Kind, it.ID), Kind: it.Kind, ID: it.ID, Title: it.Title, Size: it.Size})
	}
	return out
}

func storeHas(t Target, kind ItemKind, id string) bool {
	_, err := filestore.StatStamp(t.Root(), entryPath(kind, id))
	return !errors.Is(err, filestore.ErrNotFound)
}

func existingConnection(t Target, name string) (workspace.Connection, bool) {
	if t == nil {
		return workspace.Connection{}, false
	}
	return t.Connection(name)
}

func scopeWords(s workspace.Scope) string {
	switch s {
	case workspace.ScopeShared:
		return "team"
	case workspace.ScopeLocal:
		return "this computer"
	case workspace.ScopeUser:
		return "all your workspaces"
	}
	return string(s)
}

// ConnSummary is a one-line "where does this connection point" for a
// definition: host, database, path and scope, space-separated, never a secret.
func ConnSummary(d workspace.ConnectionDef) string {
	var parts []string
	for _, v := range []string{d.Host, d.Database, d.Path, d.Scope} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// Preview describes the package against t: every item, and which of them
// already exist there. It writes nothing. A nil t (no workspace yet: the
// launcher importing into a new one) reports no conflicts.
func (p *Package) Preview(ctx context.Context, t Target) Preview {
	pv := Preview{
		WorkspaceName: p.Manifest.WorkspaceName, CreatedAt: p.Manifest.CreatedAt, CreatedBy: p.Manifest.CreatedBy,
		Encrypted: p.Encrypted, Items: []PreviewItem{}, Connections: []PreviewConnection{}, Warnings: append([]string{}, p.Warnings...),
		MakerNotes: append([]string{}, p.MakerNotes...),
	}
	for _, it := range p.Items() {
		it.Conflict = t != nil && storeHas(t, it.Kind, it.ID)
		if it.Conflict {
			p.noteRev(ctx, t, it.Kind, it.ID)
		}
		pv.Items = append(pv.Items, it)
	}
	for _, d := range p.conns {
		c := PreviewConnection{
			Key: Key(KindConnection, d.Name), Name: d.Name, Type: d.Type, Summary: cleanText(ConnSummary(d), maxTextRunes),
			Details: ConnectionDetails(d), Risks: ConnectionRisks(d), AskedWritable: p.askedWrite[d.Name],
		}
		if ex, ok := existingConnection(t, d.Name); t != nil && ok {
			c.Conflict, c.ConflictScope = true, scopeWords(ex.Scope)
			c.RenamedTo = uniqueConnectionName(t, d.Name)
		}
		pv.Connections = append(pv.Connections, c)
	}
	return pv
}

// Import writes the package into t through its stores. A failure in one item
// is recorded on that item and the rest carry on; the error return is only for
// bad options.
func (p *Package) Import(ctx context.Context, t Target, opts ImportOptions) (Result, error) {
	onConflict, connNames, err := p.checkImportOptions(opts)
	if err != nil {
		return Result{}, err
	}
	res := Result{DryRun: opts.DryRun, Warnings: append([]string{}, p.Warnings...), Outcomes: []Outcome{}}
	run := itemImporter{p: p, t: t, opts: opts, onConflict: onConflict, taken: map[string]map[string]bool{}}
	for _, it := range p.items {
		res.Outcomes = append(res.Outcomes, run.importItem(ctx, it))
	}

	defs := map[string]workspace.ConnectionDef{}
	for _, d := range p.conns {
		defs[d.Name] = d
	}
	for _, n := range connNames {
		res.Outcomes = append(res.Outcomes, p.importConnection(ctx, t, defs[n], opts, onConflict))
	}
	return res, nil
}

// checkImportOptions validates opts against the package and returns the
// default conflict action and the sorted names of the connections to import.
func (p *Package) checkImportOptions(opts ImportOptions) (Action, []string, error) {
	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = ActionRename
	}
	if onConflict == ActionImport || !onConflict.valid() {
		return "", nil, fmt.Errorf("invalid conflict action %q (want rename, overwrite or skip)", opts.OnConflict)
	}
	known := map[string]bool{}
	for _, it := range p.items {
		known[Key(it.Kind, it.ID)] = true
	}
	for _, d := range p.conns {
		known[Key(KindConnection, d.Name)] = true
	}
	for k, a := range opts.Decisions {
		if !a.valid() {
			return "", nil, fmt.Errorf("invalid action %q for %s (want rename, overwrite or skip)", a, k)
		}
		if !known[k] {
			return "", nil, fmt.Errorf("%s is not in this bundle", k)
		}
	}
	names := make([]string, 0, len(opts.IncludeConnections))
	seen := map[string]bool{}
	for _, n := range opts.IncludeConnections {
		if !known[Key(KindConnection, n)] {
			return "", nil, fmt.Errorf("this bundle has no connection named %q", n)
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return onConflict, names, nil
}

// itemImporter writes the notebooks, queries and skills of one Import,
// remembering the final ids the run has used so two items never land on one.
type itemImporter struct {
	p          *Package
	t          Target
	opts       ImportOptions
	onConflict Action
	taken      map[string]map[string]bool // final ids this run uses, per kind (dry run or not)
}

func (r *itemImporter) isTaken(kind ItemKind, id string) bool { return r.taken[string(kind)][id] }

func (r *itemImporter) take(kind ItemKind, id string) {
	if r.taken[string(kind)] == nil {
		r.taken[string(kind)] = map[string]bool{}
	}
	r.taken[string(kind)][id] = true
}

// action decides what to do with it: the person's decision for the item, else
// the default for a clash, else a plain import.
func (r *itemImporter) action(it item, clash, inStore bool) Action {
	key := Key(it.Kind, it.ID)
	action := ActionImport
	if clash {
		action = r.onConflict
	}
	if d, ok := r.opts.Decisions[key]; ok && (clash || d == ActionSkip) {
		action = d
	}
	if action == ActionImport && clash {
		action = r.onConflict
	}
	// Only an item that is in the workspace can be replaced; one that
	// merely clashes with another item of this run is kept beside it.
	if action == ActionOverwrite && !inStore {
		action = ActionRename
	}
	return action
}

func (r *itemImporter) importItem(ctx context.Context, it item) Outcome {
	o := Outcome{Key: Key(it.Kind, it.ID), Kind: it.Kind, ID: it.ID}
	inStore := storeHas(r.t, it.Kind, it.ID)
	clash := inStore || r.isTaken(it.Kind, it.ID)
	action := r.action(it, clash, inStore)
	switch {
	case action == ActionSkip:
		o.Result = "skipped"
	case action == ActionRename && clash:
		final := filestore.UniqueSlug(it.ID, func(c string) bool { return r.isTaken(it.Kind, c) || storeHas(r.t, it.Kind, c) })
		r.take(it.Kind, final)
		o.Result, o.FinalID = r.p.write(ctx, r.t, it, final, false, r.opts.DryRun, &o)
		if o.Error == "" {
			o.Result = "renamed"
		}
	default:
		r.take(it.Kind, it.ID)
		// A new item, or an overwrite of the one that is there.
		o.Result, o.FinalID = r.p.write(ctx, r.t, it, it.ID, action == ActionOverwrite && inStore, r.opts.DryRun, &o)
	}
	return o
}

// write stores one item under finalID. overwrite is a compare-and-write
// against the revision just read, so an item that changed in between is a
// conflict, never a silent clobber.
func (p *Package) write(ctx context.Context, t Target, it item, finalID string, overwrite, dry bool, o *Outcome) (string, string) {
	result := "created"
	if overwrite {
		result = "overwritten"
	}
	if dry {
		return result, finalID
	}
	var (
		final = finalID
		err   error
	)
	switch it.Kind {
	case KindNotebook:
		final, err = p.writeNotebook(ctx, t, it, finalID, overwrite)
	case KindQuery:
		final, err = p.writeQuery(ctx, t, it, finalID, overwrite)
	case KindSkill:
		final, err = p.writeSkill(ctx, t, it, finalID, overwrite)
	default:
		err = fmt.Errorf("unknown kind %q", it.Kind)
	}
	if err != nil {
		o.Error = err.Error()
		return "failed", ""
	}
	return result, final
}

func (p *Package) writeNotebook(ctx context.Context, t Target, it item, id string, overwrite bool) (string, error) {
	nb, _, err := CheckNotebook(it.Data)
	if err != nil {
		return "", err
	}
	nb.ID, nb.Rev = id, ""
	// KeepID: an id means the same in the bundle and here, even nb_<uuid>.
	opts := []notebooks.SaveOption{notebooks.KeepID()}
	if overwrite {
		rev := p.revFor(ctx, t, KindNotebook, id)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing notebook %q: it could not be read", id)
		}
		opts = append(opts, notebooks.IfRev(rev))
	}
	raw, err := json.Marshal(nb)
	if err != nil {
		return "", fmt.Errorf("failed to encode notebook %q: %w", id, err)
	}
	saved, _, err := t.Notebooks().Save(ctx, raw, opts...)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}

func (p *Package) writeQuery(ctx context.Context, t Target, it item, id string, overwrite bool) (string, error) {
	q, err := queries.Decode(it.ID, it.Data)
	if err != nil {
		return "", err
	}
	q.ID = id
	if overwrite {
		rev := p.revFor(ctx, t, KindQuery, id)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing query %q: it could not be read", id)
		}
		q.Rev = rev
		if err := t.Queries().Update(ctx, &q, queries.WithRev(rev)); err != nil {
			return "", err
		}
		return q.ID, nil
	}
	if err := t.Queries().Create(ctx, &q); err != nil {
		return "", err
	}
	return q.ID, nil
}

func (p *Package) writeSkill(ctx context.Context, t Target, it item, name string, overwrite bool) (string, error) {
	st, err := skills.Decode(it.ID, it.Data, nil)
	if err != nil {
		return "", err
	}
	// Whatever the file claimed (Decode already refuses "built-in"), a skill
	// that arrives in a bundle is marked as imported.
	st.Source = skills.SourceImport
	st.Name = name
	var opts []skills.PutOption
	if overwrite {
		rev := p.revFor(ctx, t, KindSkill, name)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing skill %q: it could not be read", name)
		}
		st.Rev = rev
		opts = append(opts, skills.WithRev(rev))
	}
	if err := t.Skills().Put(ctx, &st, opts...); err != nil {
		return "", err
	}
	return st.Name, nil
}

func (p *Package) importConnection(ctx context.Context, t Target, d workspace.ConnectionDef, opts ImportOptions, onConflict Action) Outcome {
	key := Key(KindConnection, d.Name)
	o := Outcome{Key: key, Kind: KindConnection, ID: d.Name}
	ex, conflict := existingConnection(t, d.Name)
	action := ActionImport
	if conflict {
		action = onConflict
	}
	if dec, ok := opts.Decisions[key]; ok && (conflict || dec == ActionSkip) {
		action = dec
	}
	if action == ActionImport && conflict {
		action = onConflict
	}
	final := d.Name
	var err error
	switch {
	case action == ActionSkip:
		o.Result = "skipped"
		return o
	case action == ActionOverwrite && conflict:
		// Only your own "This computer" definition can be replaced: replacing a
		// team or all-workspaces one would be a local override a bundle chose.
		if ex.Scope != workspace.ScopeLocal {
			o.Result, o.Error = "failed", fmt.Sprintf("a %s connection named %q exists and a bundle cannot replace it; rename the imported one or skip it", scopeWords(ex.Scope), d.Name)
			return o
		}
		o.Result = "overwritten"
		// Keep what the person chose for write access, and drop their sign-in
		// choice and saved credential for this name: the new definition may
		// point somewhere else, and it arrives needing credentials, as the
		// docs say. (RemoveConnection clears them; AddConnection puts it back.)
		for _, cur := range t.Local().Connections {
			if cur.Name == d.Name {
				d.ReadOnly = cur.ReadOnly
			}
		}
		if !opts.DryRun {
			if err = t.RemoveConnection(ctx, workspace.ScopeLocal, d.Name); err == nil {
				err = t.AddConnection(workspace.ScopeLocal, d)
			}
		}
	case action == ActionRename && conflict:
		final = uniqueConnectionName(t, d.Name)
		d.Name = final
		o.Result = "renamed"
		if !opts.DryRun {
			err = t.AddConnection(workspace.ScopeLocal, d)
		}
	default:
		o.Result = "created"
		if !opts.DryRun {
			err = t.AddConnection(workspace.ScopeLocal, d)
		}
	}
	if err != nil {
		o.Result, o.Error = "failed", err.Error()
		return o
	}
	o.FinalID = final
	return o
}

func uniqueConnectionName(t Target, name string) string {
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("_%d", i)
		base := name
		if len(base)+len(suffix) > 63 {
			base = base[:63-len(suffix)]
		}
		cand := base + suffix
		if _, ok := existingConnection(t, cand); !ok && session.ValidateName(cand) == nil {
			return cand
		}
	}
}

// NotebookPackage wraps one notebook file (a .vnb, or Studio's single-notebook
// export with its "kind" wrapper) as a package of one item, so a lone notebook
// goes through exactly the checks, conflict handling and stores a bundled one
// does. name is the file's name; its stem becomes the id when it is a valid
// one, else the id comes from the title.
func NotebookPackage(name string, data []byte) (*Package, error) {
	if len(data) > MaxEntryBytes {
		return nil, fmt.Errorf("%w: the notebook file is %d bytes, over the %d byte limit", ErrTooLarge, len(data), MaxEntryBytes)
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("not a notebook file: %w", err)
	}
	if kind, ok := wrapper["kind"]; ok {
		var k string
		_ = json.Unmarshal(kind, &k)
		if k != notebooks.FileKind {
			return nil, fmt.Errorf("not a Vibe notebook file (kind %q)", cleanText(k, 40))
		}
		if inner, ok := wrapper["notebook"]; ok {
			data = inner
		}
	}
	nb, _, err := CheckNotebook(data)
	if err != nil {
		return nil, err
	}
	canon, err := notebooks.EncodeFile(nb)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if !filestore.ValidID(id) {
		id = filestore.Slug(nb.Title)
	}
	sum := sha256.Sum256(canon)
	e := Entry{Path: entryPath(KindNotebook, id), Kind: KindNotebook, ID: id, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(canon))}
	return &Package{
		Manifest: Manifest{Kind: Kind, FormatVersion: FormatVersion, Contents: []Entry{e}},
		items:    []item{{Entry: e, Title: cleanText(nb.Title, maxTextRunes), Data: canon}},
		revs:     map[string]string{},
	}, nil
}
