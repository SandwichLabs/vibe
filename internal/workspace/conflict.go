package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// Conflict kinds and actions (ConflictRequest.Kind, .Action).
const (
	ConflictNotebook = "notebook"
	ConflictQuery    = "query"
	ConflictSkill    = "skill"

	ConflictReload    = "reload"
	ConflictOverwrite = "overwrite"
	ConflictSaveCopy  = "saveCopy"
)

// ConflictRequest resolves a save refused because the file changed on disk.
type ConflictRequest struct {
	Kind   string `json:"kind"`   // notebook | query | skill
	ID     string `json:"id"`     // notebook id, query id or skill name
	Action string `json:"action"` // reload | overwrite | saveCopy
	// Data is the local edit. Notebook: the same document SaveNotebook takes.
	// Query: {title, sql, description}. Skill: {description, instructions,
	// tableName}.
	Data map[string]any `json:"data"`
	// Rev is the revision on disk that the conflict banner was raised against.
	// "Keep mine" only overwrites that revision: a file that changed again
	// meanwhile is a fresh conflict (the store refuses with filestore.ErrConflict).
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

// conflictTarget is one kind of item a conflict can be about. ResolveConflict
// picks the target by kind and the method by action, so the action rules read
// once, in one place.
type conflictTarget interface {
	// reload reports the disk version (the caller then re-reads it).
	reload(ctx context.Context, id string) (*ConflictResult, error)
	// overwrite writes the local edit over the disk version, but only the
	// revision the banner was raised against; an item deleted on disk is
	// written back.
	overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error)
	// saveCopy writes the local edit as a new item that leaves the disk one alone.
	saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error)
}

// ResolveConflict settles a save that was refused because the file changed on
// disk: "reload" keeps the disk version, "overwrite" writes the local edit over
// it, and "saveCopy" writes the local edit as a new item (a notebook or query
// named "<title> (copy)", a skill named "<name>-copy"). A write whose revision
// no longer matches the disk is refused with filestore.ErrConflict, so a
// second outside edit is never overwritten silently.
func ResolveConflict(ctx context.Context, ws Workspace, sess session.Session, req ConflictRequest) (*ConflictResult, error) {
	switch req.Action {
	case ConflictReload, ConflictOverwrite, ConflictSaveCopy:
	default:
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
	var target conflictTarget
	switch req.Kind {
	case ConflictNotebook:
		target = notebookConflicts{store: ws.Notebooks()}
	case ConflictQuery:
		target = queryConflicts{store: ws.Queries()}
	case ConflictSkill:
		target = skillConflicts{store: ws.Skills(), sess: sess}
	default:
		return nil, fmt.Errorf("unknown kind %q", req.Kind)
	}
	switch req.Action {
	case ConflictReload:
		return target.reload(ctx, req.ID)
	case ConflictOverwrite:
		return target.overwrite(ctx, req)
	}
	return target.saveCopy(ctx, req)
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

// ---- notebooks ----

type notebookConflicts struct{ store notebooks.Store }

func (c notebookConflicts) reload(ctx context.Context, id string) (*ConflictResult, error) {
	nb, _, err := c.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: nb.ID, Rev: nb.Rev, Title: nb.Title}, nil
}

func (c notebookConflicts) overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	raw := cloneData(req.Data)
	raw["id"] = req.ID
	delete(raw, "rev")
	opt := notebooks.Overwrite()
	if req.Rev != "" {
		opt = notebooks.IfRev(req.Rev) // only the revision the banner was raised against
	}
	nb, _, err := c.store.Save(ctx, raw, opt)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: nb.ID, Rev: nb.Rev, Title: nb.Title}, nil
}

func (c notebookConflicts) saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	raw := cloneData(req.Data)
	delete(raw, "id")
	delete(raw, "rev")
	raw["title"] = copyTitle(str(raw, "title"))
	nb, _, err := c.store.Save(ctx, raw)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: nb.ID, Rev: nb.Rev, Title: nb.Title}, nil
}

func cloneData(m map[string]any) map[string]any {
	raw := map[string]any{}
	for k, v := range m {
		raw[k] = v
	}
	return raw
}

// ---- queries ----

type queryConflicts struct{ store queries.Store }

func (c queryConflicts) reload(ctx context.Context, id string) (*ConflictResult, error) {
	q, err := c.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: q.ID, Rev: q.Rev, Title: q.Title}, nil
}

// edited builds the query to write: what the editor sends, on top of what it
// does not edit (parameters, description, engine) from the disk version.
func (c queryConflicts) edited(ctx context.Context, req ConflictRequest) (*queries.Query, error) {
	q := &queries.Query{Title: str(req.Data, "title"), SQL: str(req.Data, "sql"), Engine: "duckdb", Parameters: []queries.Param{}}
	if base, err := c.store.Get(ctx, req.ID); err == nil && base != nil {
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
	return q, nil
}

func (c queryConflicts) overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	q, err := c.edited(ctx, req)
	if err != nil {
		return nil, err
	}
	q.ID = req.ID
	var opt queries.WriteOption = queries.Overwrite()
	if req.Rev != "" {
		opt = queries.WithRev(req.Rev) // only the revision the banner was raised against
	}
	if err := c.store.Update(ctx, q, opt); err != nil {
		if errors.Is(err, queries.ErrNotFound) { // deleted on disk: write it back
			q.ID = ""
			err = c.store.Create(ctx, q)
		}
		if err != nil {
			return nil, err
		}
	}
	return &ConflictResult{ID: q.ID, Rev: q.Rev, Title: q.Title}, nil
}

func (c queryConflicts) saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	q, err := c.edited(ctx, req)
	if err != nil {
		return nil, err
	}
	q.Title = copyTitle(q.Title)
	if err := c.store.Create(ctx, q); err != nil {
		return nil, err
	}
	return &ConflictResult{ID: q.ID, Rev: q.Rev, Title: q.Title}, nil
}

// ---- skills ----

type skillConflicts struct {
	store skills.Store
	sess  session.Session
}

// current is the skill on disk, nil when it is gone (an error other than "not
// found" stops the resolution).
func (c skillConflicts) current(ctx context.Context, name string) (*skills.Stored, error) {
	cur, err := c.store.Get(ctx, name)
	if err != nil && !errors.Is(err, skills.ErrNotFound) {
		return nil, err
	}
	return cur, nil
}

func (c skillConflicts) reload(ctx context.Context, name string) (*ConflictResult, error) {
	cur, err := c.current(ctx, name)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, skills.ErrNotFound
	}
	return &ConflictResult{ID: cur.Name, Rev: cur.Rev, Title: cur.Name}, nil
}

func (c skillConflicts) overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	cur, err := c.current(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	var putOpts []skills.PutOption
	rev := ""
	switch {
	case cur == nil:
		putOpts = append(putOpts, skills.Overwrite()) // deleted on disk: write it back
	case req.Rev != "":
		rev = req.Rev // only the revision the banner was raised against, as for queries and notebooks
	default:
		rev = cur.Rev // no revision known: replace whatever is on disk now
	}
	return c.save(req.ID, rev, req, putOpts...)
}

func (c skillConflicts) saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	if _, err := c.current(ctx, req.ID); err != nil {
		return nil, err
	}
	name := req.ID + "-copy"
	for n := 2; n < 100; n++ { // never overwrite an earlier copy
		if ex, gerr := c.store.Get(ctx, name); gerr != nil || ex == nil {
			break
		}
		name = fmt.Sprintf("%s-copy-%d", req.ID, n)
	}
	return c.save(name, "", req)
}

func (c skillConflicts) save(name, rev string, req ConflictRequest, opts ...skills.PutOption) (*ConflictResult, error) {
	newRev, err := skills.SaveUserSkillAt(c.store, c.sess, name,
		str(req.Data, "description"), str(req.Data, "instructions"), str(req.Data, "tableName"), rev, opts...)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: name, Rev: newRev, Title: name}, nil
}
