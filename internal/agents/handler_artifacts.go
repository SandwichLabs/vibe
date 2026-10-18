package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/jsonenc"
	"github.com/SandwichLabs/vibe/internal/secrets"
)

// --- write_artifact / list_artifacts / get_artifact (epic #162, step 3) ---
//
// The agent authors a data artifact (artifacts/<name>/artifact.yaml plus
// view.a2ui.json, or view.js for view: script) and can read back what is
// stored. It can write a script view but never allow one: allow, deny and
// revoke are the person's, in Studio or the CLI, and no tool or MCP method
// reaches them. It never reads an
// artifact's RESULTS: there is no tool that runs an artifact query for the
// agent. The run path (RunArtifactQuery) belongs to Studio's UI, so an artifact
// cannot widen what the agent's own run_query could read.

// ArtifactsProvider is implemented by a WorkspaceAccess that has a workspace
// with an artifacts/ folder. It is optional (as DataDirProvider is) so the
// WorkspaceAccess doubles of other packages need not change; without it the
// artifact tools say so.
type ArtifactsProvider interface {
	Artifacts() artifacts.Store
}

// artifactSourceBudgetRunes caps one query's SQL in get_artifact, as
// get_notebook caps a cell's source. artifactViewBudgetBytes caps the view; a
// larger one is summarised.
const (
	artifactSourceBudgetRunes = notebookCellSourceBudgetRunes
	artifactViewBudgetBytes   = 2 * RowResultBudgetChars
	errArtifactsUnavailable   = "data artifacts are not available in this workspace"
)

func (h *Handler) artifactStore() (artifacts.Store, error) {
	if h.access != nil {
		if p, ok := h.access.(ArtifactsProvider); ok {
			if s := p.Artifacts(); s != nil {
				return s, nil
			}
		}
	}
	return nil, errors.New(errArtifactsUnavailable)
}

// redactForAgent scrubs credential-shaped text from anything handed to the
// agent that came from a connection, the policy or the store.
func redactForAgent(s string) string { return secrets.RedactPatterns(s) }

// absPathRe finds an absolute filesystem path (POSIX or Windows) in text.
var absPathRe = regexp.MustCompile(`(?:^|[\s'"(=])((?:/|[A-Za-z]:[\\/])[^\s'"):,;]+)`)

// scrubStoreText is for text that came from the file system or the store: it
// redacts credentials, replaces any absolute path with <path> (an I/O error
// wraps the real one) and caps the length.
func scrubStoreText(s string) string {
	s = redactForAgent(s)
	s = absPathRe.ReplaceAllStringFunc(s, func(m string) string {
		loc := absPathRe.FindStringSubmatchIndex(m)
		return m[:loc[2]] + "<path>"
	})
	out, cut := truncateRunes(s, 400)
	if cut {
		out += "..."
	}
	return out
}

// artifactErrorResponse maps a store error to a response that shows the model
// what to fix without echoing a path: our own structural refusals pass through
// (scrubbed); a missing artifact, a conflict and anything else get a fixed
// message, and the real error goes to the log.
func (h *Handler) artifactErrorResponse(id any, name, action string, err error) *Response {
	switch {
	case errors.Is(err, artifacts.ErrNotFound):
		return NewErrorResponse(id, ErrInvalidParams, fmt.Sprintf("there is no artifact named %q (see list_artifacts)", name))
	case errors.Is(err, artifacts.ErrInvalid):
		return NewErrorResponse(id, ErrInvalidParams, scrubStoreText(err.Error()))
	}
	h.logger.Warn("artifact store error", "artifact", name, "action", action, "error", err)
	return NewErrorResponse(id, ErrInternal, fmt.Sprintf("failed to %s artifact %q", action, name))
}

// --- list_artifacts ---------------------------------------------------------

func (h *Handler) handleListArtifacts(ctx context.Context, req *Request) *Response {
	store, err := h.artifactStore()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	list, err := store.List(ctx)
	if err != nil {
		h.logger.Warn("artifact store error", "action", "list", "error", err)
		return NewErrorResponse(req.ID, ErrInternal, "failed to list artifacts")
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		row := map[string]any{"name": a.Name}
		if a.Error != "" {
			row["error"] = scrubStoreText(a.Error)
			out = append(out, row)
			continue
		}
		row["title"] = a.Manifest.Title
		row["description"] = a.Manifest.Description
		row["view"] = viewKind(a.Manifest.View)
		row["parameters"] = len(a.Manifest.Parameters)
		row["queries"] = len(a.Manifest.Queries)
		row["rev"] = a.Rev
		row["updated_at"] = a.UpdatedAt
		out = append(out, row)
	}
	return NewResponse(req.ID, map[string]any{"artifacts": out})
}

func viewKind(k string) string {
	if k == "" {
		return artifacts.ViewA2UI
	}
	return k
}

// --- get_artifact -----------------------------------------------------------

func (h *Handler) handleGetArtifact(ctx context.Context, req *Request) *Response {
	name := strings.TrimSpace(stringParam(req.Params["name"]))
	if !filestore.ValidID(name) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("there is no artifact named %q (see list_artifacts)", name))
	}
	store, err := h.artifactStore()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	a, err := store.Get(ctx, name)
	if err != nil {
		return h.artifactErrorResponse(req.ID, name, "read", err)
	}
	manifest, err := manifestForAgent(&a.Manifest)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, "failed to read artifact "+strconv.Quote(name))
	}
	res := map[string]any{
		"name": a.Name, "rev": a.Rev, "updated_at": a.UpdatedAt, "manifest": manifest,
	}
	switch {
	case a.Manifest.View == artifacts.ViewScript:
		addScriptSource(res, a.View)
	case len(a.View) <= artifactViewBudgetBytes:
		var view any
		if err := json.Unmarshal(a.View, &view); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, "failed to read artifact "+strconv.Quote(name))
		}
		res["view"] = view
	default:
		res["view_truncated"] = true
		res["view_summary"] = viewSummary(a.View)
	}
	return NewResponse(req.ID, res)
}

// addScriptSource returns view.js the way a long query's SQL is returned: whole,
// or (over artifactViewBudgetBytes, the view cap) not at all, never as a prefix, so a
// partial script cannot exist for the agent to write back. write_artifact's
// keep_view_source keeps the stored script.
func addScriptSource(res map[string]any, src []byte) {
	if len(src) > artifactViewBudgetBytes {
		res["view_source"] = nil
		res["view_source_omitted"] = true
		res["view_source_length"] = utf8.RuneCount(src)
		return
	}
	res["view_source"] = string(src)
}

// manifestForAgent renders a manifest as plain data. A query whose SQL is over
// artifactSourceBudgetRunes is returned WITHOUT its SQL ("sql": null,
// "sql_omitted": true, "sql_length"), never as a prefix, so a partial query
// cannot exist for the agent to write back; write_artifact's keep_sql keeps
// the stored text.
func manifestForAgent(m *artifacts.Manifest) (map[string]any, error) {
	head := *m
	head.Queries = nil
	raw, err := json.Marshal(head)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	queries := make([]map[string]any, 0, len(m.Queries))
	for _, q := range m.Queries {
		row := map[string]any{"name": q.Name, "sql": q.SQL, "uses": q.Uses}
		if q.Connection != "" {
			row["connection"] = q.Connection
		}
		if n := utf8.RuneCountInString(q.SQL); n > artifactSourceBudgetRunes {
			row["sql"] = nil
			row["sql_omitted"] = true
			row["sql_length"] = n
		}
		queries = append(queries, row)
	}
	out["queries"] = queries
	return out, nil
}

// viewSummary describes a view too large to return: its title and how many
// components of each type it has.
func viewSummary(view []byte) map[string]any {
	var page struct {
		Title      string `json:"title"`
		Components []struct {
			Type string `json:"type"`
		} `json:"components"`
	}
	_ = json.Unmarshal(view, &page)
	types := map[string]int{}
	for _, c := range page.Components {
		types[c.Type]++
	}
	return map[string]any{"title": page.Title, "component_count": len(page.Components), "component_types": types}
}

// --- write_artifact ---------------------------------------------------------

func (h *Handler) handleWriteArtifact(ctx context.Context, req *Request) *Response {
	name := strings.TrimSpace(stringParam(req.Params["name"]))
	if !filestore.ValidID(name) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("artifact name %q must be 1 to 64 lowercase letters, digits, - or _", name))
	}
	store, err := h.artifactStore()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	m, keep, err := manifestFromParam(req.Params["manifest"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, "manifest: "+err.Error())
	}
	rev, err := revParam(req.Params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	view, err := viewFromParams(ctx, store, name, rev, m, req.Params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, "view: "+err.Error())
	}
	if err := keepStoredSQL(ctx, store, name, rev, m, keep); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, "manifest: "+err.Error())
	}
	// Structure first (names, types, defaults, caps, the view against the
	// manifest), then the SQL policy, then the compare-and-write.
	if err := artifacts.Validate(m); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText(err.Error()))
	}
	if err := artifacts.ValidateView(m, view); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText(err.Error()))
	}
	if err := h.VetArtifact(ctx, m); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	// The revision the agent read (get_artifact / list_artifacts), or none.
	// WithRev("") is create-only: the store's own last-seen revision must never
	// stand in for a rev the agent did not supply, or the agent could replace a
	// person's version of an artifact it never read (the store sees every
	// artifact Studio or a listing has loaded).
	a := &artifacts.Artifact{Name: name, Manifest: *m, View: view}
	if err := store.Put(ctx, a, artifacts.WithRev(rev)); err != nil {
		if errors.Is(err, artifacts.ErrConflict) {
			return NewErrorResponse(req.ID, ErrInvalidParams, conflictMessage(name, rev))
		}
		return h.artifactErrorResponse(req.ID, name, "save", err)
	}
	h.logger.Info("agent wrote artifact", "artifact", name, "queries", len(m.Queries))
	res := map[string]any{
		"name": name, "saved": true, "rev": a.Rev, "queries_checked": len(m.Queries),
	}
	if m.View == artifacts.ViewScript {
		res["note"] = "the script view is stored but does not run until the person allows it in Studio; it asks again after every edit"
	}
	return NewResponse(req.ID, res)
}

func conflictMessage(name, rev string) string {
	if rev == "" {
		return fmt.Sprintf("an artifact named %q already exists, so nothing was written. Call get_artifact to read it, then send its \"rev\" with your changes (or pick another name).", name)
	}
	return fmt.Sprintf("artifact %q changed since the rev you sent (a person or another tool edited it), so nothing was written. Call get_artifact again, merge your change into what it returns, and send the new \"rev\".", name)
}

// manifestFromParam turns the tool's manifest object into a Manifest. It is
// strict: an unknown key is an error. A scalar default (90, true) is accepted
// and written as its text, since every default is stored as a string.
//
// A query may carry {"name": X, "keep_sql": true} instead of sql (what
// get_artifact returns as sql_omitted for a long query): those names come back
// in keep and the handler substitutes the stored SQL.
func manifestFromParam(v any) (m *artifacts.Manifest, keep []string, err error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, nil, errors.New("must be an object with title, parameters and queries")
	}
	if keep, err = takeKeepSQL(obj); err != nil {
		return nil, nil, err
	}
	if ps, ok := obj["parameters"].([]any); ok {
		for _, p := range ps {
			if pm, ok := p.(map[string]any); ok {
				pm["default"] = scalarText(pm["default"])
			}
		}
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, errors.New("is not valid JSON data")
	}
	if len(raw) > artifacts.MaxManifestBytes {
		return nil, nil, fmt.Errorf("is over the %d byte limit", artifacts.MaxManifestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out artifacts.Manifest
	if err := dec.Decode(&out); err != nil {
		return nil, nil, err
	}
	if out.Version == 0 {
		out.Version = artifacts.Version
	}
	return &out, keep, nil
}

// takeKeepSQL removes get_artifact's read-only markers (sql_omitted,
// sql_length) and the keep_sql flag from each query object, by inspecting the
// maps, and returns the names that asked to keep their stored SQL. keep_sql
// next to a non-empty sql is ambiguous and refused.
func takeKeepSQL(obj map[string]any) ([]string, error) {
	qs, _ := obj["queries"].([]any)
	var keep []string
	for _, q := range qs {
		qm, ok := q.(map[string]any)
		if !ok {
			continue
		}
		delete(qm, "sql_omitted")
		delete(qm, "sql_length")
		flag, present := qm["keep_sql"]
		delete(qm, "keep_sql")
		if on, _ := flag.(bool); !present || !on {
			continue
		}
		name := stringParam(qm["name"])
		if sqlText, has := qm["sql"]; has && sqlText != nil && stringParam(sqlText) != "" {
			return nil, fmt.Errorf("query %q has both keep_sql and sql: send one", name)
		}
		keep = append(keep, name)
	}
	return keep, nil
}

// keepStoredSQL replaces each keep_sql query's SQL with the stored text,
// byte for byte, before validation and vetting. It is only valid with a rev
// and a stored query of that name.
func keepStoredSQL(ctx context.Context, store artifacts.Store, name, rev string, m *artifacts.Manifest, keep []string) error {
	if len(keep) == 0 {
		return nil
	}
	if rev == "" {
		return errors.New("keep_sql needs the rev of the artifact you read (get_artifact), since it keeps that stored SQL")
	}
	cur, err := store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("keep_sql needs the stored artifact %q to exist and be readable", name)
	}
	for _, k := range keep {
		stored := queryByName(cur.Manifest.Queries, k)
		if stored == nil {
			return fmt.Errorf("keep_sql: the stored artifact has no query named %q", k)
		}
		if in := queryByName(m.Queries, k); in != nil {
			in.SQL = stored.SQL
		}
	}
	return nil
}

func queryByName(qs []artifacts.Query, name string) *artifacts.Query {
	for i := range qs {
		if qs[i].Name == name {
			return &qs[i]
		}
	}
	return nil
}

// scalarText renders a number or boolean default as the text the manifest
// stores; anything else is returned as it is (and refused by validation).
func scalarText(v any) any {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return v
}

// viewFromParams picks the view file's bytes by the manifest's view kind:
// view (an A2UI page) for a2ui, view_source (the view.js text) or
// keep_view_source for script. The other kind's parameter is refused, not
// ignored.
func viewFromParams(ctx context.Context, store artifacts.Store, name, rev string, m *artifacts.Manifest, params map[string]any) ([]byte, error) {
	_, hasView := params["view"]
	_, hasSource := params["view_source"]
	keep, _ := params["keep_view_source"].(bool)
	if m.View != artifacts.ViewScript {
		if hasSource || keep {
			return nil, errors.New("view_source is only for a manifest with view: script")
		}
		return viewFromParam(params["view"])
	}
	switch {
	case hasView:
		return nil, errors.New("a manifest with view: script takes view_source (the view.js text), not view")
	case keep && hasSource:
		return nil, errors.New("send view_source or keep_view_source, not both")
	case keep:
		return storedScript(ctx, store, name, rev)
	}
	src, ok := params["view_source"].(string)
	if !ok {
		return nil, errors.New("view_source must be the text of view.js")
	}
	if len(src) > artifacts.MaxScriptBytes {
		return nil, fmt.Errorf("view_source is over the %d byte limit", artifacts.MaxScriptBytes)
	}
	return []byte(src), nil
}

// storedScript is keep_view_source: the stored view.js byte for byte, only
// with the rev of the artifact the agent read.
func storedScript(ctx context.Context, store artifacts.Store, name, rev string) ([]byte, error) {
	if rev == "" {
		return nil, errors.New("keep_view_source needs the rev of the artifact you read (get_artifact)")
	}
	cur, err := store.Get(ctx, name)
	if err != nil || cur.Manifest.View != artifacts.ViewScript {
		return nil, fmt.Errorf("keep_view_source needs the stored artifact %q to exist with view: script", name)
	}
	return cur.View, nil
}

// viewFromParam renders the tool's view object as view.a2ui.json bytes.
func viewFromParam(v any) ([]byte, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("must be an A2UI page object: {\"title\": ..., \"components\": [...]}")
	}
	out, err := jsonenc.Encode(obj, "  ")
	if err != nil {
		return nil, errors.New("is not valid JSON data")
	}
	return out, nil
}

// revParam reads the optional rev: absent is "", present must be a string.
func revParam(params map[string]any) (string, error) {
	v, present := params["rev"]
	if !present || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", errors.New("rev must be a string (the \"rev\" get_artifact returned)")
	}
	return s, nil
}

// artifactQueriesText lists a write_artifact manifest's queries for the
// approval card, capped like a run_query statement.
func artifactQueriesText(manifest any) string {
	obj, _ := manifest.(map[string]any)
	qs, _ := obj["queries"].([]any)
	var b strings.Builder
	for _, q := range qs {
		qm, _ := q.(map[string]any)
		if keep, _ := qm["keep_sql"].(bool); keep {
			fmt.Fprintf(&b, "-- %s (kept as stored)\n\n", stringParam(qm["name"]))
			continue
		}
		fmt.Fprintf(&b, "-- %s\n%s\n\n", stringParam(qm["name"]), stringParam(qm["sql"]))
	}
	out, cut := truncateRunes(strings.TrimSpace(b.String()), 4000)
	if cut {
		out += "\n..."
	}
	return out
}
