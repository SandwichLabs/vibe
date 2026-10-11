package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// policyEnv describes the workspace to the SQL policy: its data/ folder (when
// the container is a workspace) and the registered connections.
func (h *Handler) policyEnv() *sqlEnv {
	if h.access == nil {
		return newSQLEnv("", nil)
	}
	var conns []session.ConnectionInfo
	if s := h.access.Session(); s != nil {
		conns = s.ConnectionInfos()
	}
	dir := ""
	if p, ok := h.access.(DataDirProvider); ok {
		dir = p.DataDir()
	}
	var protected []string
	if p, ok := h.access.(ProtectedPathsProvider); ok {
		protected = p.ProtectedPaths()
	}
	return newSQLEnv(dir, conns, protected...)
}

// classify runs the agent SQL policy on raw (with its $name params) against db.
func (h *Handler) classify(ctx context.Context, db queryDB, raw string, params map[string]any) *sqlVerdict {
	return analyzeSQL(ctx, db, h.policyEnv(), raw, params)
}

// classifyIn is classify on a connection pinned to the named catalog ("" is the
// session default), so unqualified names resolve exactly as they will when the
// statement runs. The pinned connection is released before it returns.
func (h *Handler) classifyIn(ctx context.Context, connection, raw string, params map[string]any) (*sqlVerdict, error) {
	userDB := h.access.DuckDBDB()
	if userDB == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	if connection == "" {
		return h.classify(ctx, userDB, raw, params), nil
	}
	sess := h.access.Session()
	if sess == nil {
		return nil, fmt.Errorf("connections are not available here")
	}
	pinned, err := sess.Conn(ctx, session.WithCatalog(connection))
	if err != nil {
		return nil, fmt.Errorf("unknown connection %q: %w", connection, err)
	}
	defer func() { _ = pinned.Close() }()
	return h.classify(ctx, pinned.SQLConn(), raw, params), nil
}

// writeGates is the second write gate: every connection a statement changes
// must be writable. It returns the names of the targets that are not local
// files, for which the person's approval is forced (the third gate).
func writeGates(targets []sqlTarget) (nonLocal []string, err error) {
	seen := map[string]bool{}
	for _, t := range targets {
		if t.System {
			return nil, fmt.Errorf("DuckDB's system catalog cannot be changed")
		}
		if t.ReadOnly {
			return nil, fmt.Errorf("the connection %q is read-only, so the agent cannot change it (an owner can set read_only: false on the connection; a read-only database role is the real guard)", t.Catalog)
		}
		if !t.Local && !seen[t.Catalog] {
			seen[t.Catalog] = true
			nonLocal = append(nonLocal, t.Catalog)
		}
	}
	return nonLocal, nil
}

// toolTarget names the connection a record or schema tool writes, from its
// table or name parameter (qualified catalog.schema.table, or the default
// connection). ok is false for tools that write no table, and when there is no
// session to ask (the handler then refuses the unresolvable name itself).
func (h *Handler) toolTarget(ctx context.Context, req *Request) (sqlTarget, bool) {
	param := ""
	switch req.Method {
	case "create_record", "update_record", "delete_record", "bulk_insert", "add_column", "import_csv":
		param = "table"
	case "create_table", "drop_table":
		param = "name"
	default:
		return sqlTarget{}, false
	}
	name, _ := req.Params[param].(string)
	sess := h.access.Session()
	if sess == nil || strings.TrimSpace(name) == "" {
		return sqlTarget{}, false
	}
	ref, err := session.ParseTableRef(name)
	if err != nil {
		return sqlTarget{}, false
	}
	r, err := sess.Resolve(ctx, ref)
	if err != nil {
		return sqlTarget{}, false
	}
	return h.catalogTarget(ctx, sess, r.Catalog), true
}

// catalogTarget builds the write target for an attached catalog.
func (h *Handler) catalogTarget(ctx context.Context, sess session.Session, catalog string) sqlTarget {
	t := sqlTarget{Catalog: strings.ToLower(catalog)}
	// Fail closed: a catalog we cannot describe is not writable and not local.
	t.ReadOnly = true
	cats, err := sess.ListCatalogs(ctx)
	if err != nil {
		return t
	}
	for _, c := range cats {
		if strings.EqualFold(c.Name, catalog) {
			t.ReadOnly = c.ReadOnly
			t.Local = c.Type == "duckdb" || c.Type == "sqlite"
			break
		}
	}
	for _, ci := range sess.ConnectionInfos() {
		if strings.EqualFold(ci.Name, catalog) {
			t.ReadOnly = t.ReadOnly || ci.ReadOnly
			if ci.Remote {
				t.Local = false
			}
		}
	}
	return t
}

// --- list_connections ---------------------------------------------------------

func (h *Handler) handleListConnections(_ context.Context, req *Request) *Response {
	sess, errResp := h.sess(req.ID)
	if errResp != nil {
		return errResp
	}
	infos := sess.ConnectionInfos()
	out := make([]map[string]any, 0, len(infos)+1)
	for _, c := range infos {
		loc := "local"
		if c.Remote {
			loc = "remote"
		}
		m := map[string]any{
			"name":      c.Name,
			"type":      c.Type,
			"status":    string(c.Kind),
			"read_only": c.ReadOnly,
			"location":  loc,
		}
		if c.Default || strings.EqualFold(c.Name, sess.DefaultCatalog()) {
			m["default"] = true
		}
		if c.Catalogless {
			m["catalog"] = false
			m["scope"] = c.Scope
		} else {
			m["catalog"] = true
		}
		out = append(out, m)
	}
	// A legacy file has no registered connections: describe its one catalog.
	if len(infos) == 0 {
		out = append(out, map[string]any{
			"name": sess.DefaultCatalog(), "type": "duckdb", "status": string(session.StatusOK),
			"read_only": false, "location": "local", "default": true, "catalog": true,
		})
	}
	res := map[string]any{"connections": out, "default": sess.DefaultCatalog()}
	if p, ok := h.access.(DataDirProvider); ok && p.DataDir() != "" {
		res["data_dir"] = p.DataDir()
	}
	return NewResponse(req.ID, res)
}

// validConnection checks that name is a registered connection with a catalog
// (what a notebook cell's "connection" may be) and returns its canonical name.
func (h *Handler) validConnection(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	sess := h.access.Session()
	if sess == nil {
		return "", fmt.Errorf("connections are not available here")
	}
	for _, c := range sess.ConnectionInfos() {
		if strings.EqualFold(c.Name, name) {
			if c.Catalogless {
				return "", fmt.Errorf("%q is a storage connection with no catalog; read its files with read_parquet('<scope>...') in a cell on a catalog connection", c.Name)
			}
			return c.Name, nil
		}
	}
	if strings.EqualFold(name, sess.DefaultCatalog()) {
		return sess.DefaultCatalog(), nil
	}
	return "", fmt.Errorf("there is no connection named %q (see list_connections)", name)
}
