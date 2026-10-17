package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/session"
)

// Catalog is the schema snapshot completion reads: connections, their schemas,
// tables and views, and columns, plus DuckDB's function names. It holds names
// and types only: no rows, no DSN, no credential.
type Catalog struct {
	Version           int          `json:"version"`
	Generated         time.Time    `json:"generated"`
	DefaultConnection string       `json:"default_connection,omitempty"`
	Connections       []Connection `json:"connections"`
	Functions         []string     `json:"functions,omitempty"`
}

// Connection is one attached catalog.
type Connection struct {
	Name          string    `json:"name"`
	Type          string    `json:"type,omitempty"`
	DefaultSchema string    `json:"default_schema,omitempty"`
	Schemas       []Schema  `json:"schemas"`
	Updated       time.Time `json:"updated"`
}

// Schema is one schema of a connection.
type Schema struct {
	Name   string     `json:"name"`
	Tables []Relation `json:"tables"`
}

// Relation is a table or view.
type Relation struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // table | view
	Columns []Column `json:"columns"`
}

// Column is one column of a relation.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

const catalogVersion = 1

// SnapshotPath is where a workspace's catalog snapshot lives. It is machine
// local (.vibe-local is git-ignored): it reflects which connections this
// person has connected, and table names are not for the repository.
func SnapshotPath(root string) string {
	return filepath.Join(root, ".vibe-local", "catalog.json")
}

// Conn returns the connection called name (case-insensitive), or nil.
func (c *Catalog) Conn(name string) *Connection {
	if c == nil {
		return nil
	}
	for i := range c.Connections {
		if strings.EqualFold(c.Connections[i].Name, name) {
			return &c.Connections[i]
		}
	}
	return nil
}

// Schema returns the schema called name (case-insensitive), or nil.
func (c *Connection) Schema(name string) *Schema {
	if c == nil {
		return nil
	}
	for i := range c.Schemas {
		if strings.EqualFold(c.Schemas[i].Name, name) {
			return &c.Schemas[i]
		}
	}
	return nil
}

// Relation returns the table or view called name (case-insensitive), or nil.
func (s *Schema) Relation(name string) *Relation {
	if s == nil {
		return nil
	}
	for i := range s.Tables {
		if strings.EqualFold(s.Tables[i].Name, name) {
			return &s.Tables[i]
		}
	}
	return nil
}

// Load reads a snapshot. A missing file is an error satisfying
// errors.Is(err, os.ErrNotExist).
func Load(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("failed to read the catalog snapshot: %w", err)
	}
	if c.Version != catalogVersion {
		return nil, fmt.Errorf("catalog snapshot version %d is not %d", c.Version, catalogVersion)
	}
	return &c, nil
}

// Save writes the snapshot atomically (temp file in the same directory, then
// rename over path), mode 0600. A symbolic link at path is replaced, never
// written through.
func (c *Catalog) Save(path string) error {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to encode the catalog snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	if err := filestore.WriteFileAtomicExact(path, b, 0o600); err != nil {
		return fmt.Errorf("failed to write the catalog snapshot: %w", err)
	}
	return nil
}

// resolveSnapshot is SnapshotPath for writing: the folder is created 0700, and
// a .vibe-local (or catalog.json) that is a symbolic link out of the workspace,
// as a cloned repository could ship, is refused.
func resolveSnapshot(root string) (string, error) {
	path, err := filestore.Resolve(root, filepath.ToSlash(filepath.Join(".vibe-local", "catalog.json")))
	if err != nil {
		return "", fmt.Errorf("refusing to write the catalog snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// Merge returns c with every connection in fresh replacing the one of the same
// name. Connections fresh does not mention are kept, so a snapshot taken while
// only some connections are connected never forgets the others.
func (c *Catalog) Merge(fresh *Catalog) *Catalog {
	out := *fresh
	if c == nil {
		return &out
	}
	seen := map[string]bool{}
	for _, conn := range fresh.Connections {
		seen[strings.ToLower(conn.Name)] = true
	}
	out.Connections = append([]Connection{}, fresh.Connections...)
	for _, conn := range c.Connections {
		if !seen[strings.ToLower(conn.Name)] {
			out.Connections = append(out.Connections, conn)
		}
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].Name < out.Connections[j].Name })
	if len(out.Functions) == 0 {
		out.Functions = c.Functions
	}
	return &out
}

// Build snapshots every connection of sess that is connected now. It never
// dials: connecting is the caller's decision (connections connect on first
// use), so a connection that is not connected is left out, and Merge keeps
// whatever an earlier snapshot knew about it.
func Build(ctx context.Context, sess session.Session) (*Catalog, error) {
	c := &Catalog{Version: catalogVersion, Generated: time.Now().UTC(), DefaultConnection: sess.DefaultCatalog()}
	var errs []error
	for _, st := range sess.Connections() {
		if st.Kind != session.StatusOK || (builtinCatalog(st.Name) && st.Name != c.DefaultConnection) {
			continue
		}
		conn, err := buildConnection(ctx, sess, st)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", st.Name, err))
			continue
		}
		c.Connections = append(c.Connections, conn)
	}
	fns, err := functionNames(ctx, sess)
	if err != nil {
		errs = append(errs, err)
	}
	c.Functions = fns
	sort.Slice(c.Connections, func(i, j int) bool { return c.Connections[i].Name < c.Connections[j].Name })
	return c, errors.Join(errs...)
}

// columnsSQL lists one catalog's columns with their relation's kind in a
// single round trip (information_schema works for every attached catalog type).
const columnsSQL = `
SELECT c.table_schema, c.table_name, t.table_type, c.column_name, c.data_type
  FROM information_schema.columns c
  JOIN information_schema.tables t
    ON t.table_catalog = c.table_catalog AND t.table_schema = c.table_schema AND t.table_name = c.table_name
 WHERE c.table_catalog = $1
 ORDER BY c.table_schema, c.table_name, c.ordinal_position`

func buildConnection(ctx context.Context, sess session.Session, st session.Status) (Connection, error) {
	conn := Connection{Name: st.Name, Type: st.Type, Updated: time.Now().UTC()}
	if ds, err := sess.DefaultSchema(ctx, st.Name); err == nil {
		conn.DefaultSchema = ds
	}
	rows, err := sess.DB().QueryContext(ctx, columnsSQL, st.Name)
	if err != nil {
		return conn, fmt.Errorf("failed to list columns: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var sch *Schema
	var rel *Relation
	for rows.Next() {
		var schema, table, typ, col, dtype string
		if err := rows.Scan(&schema, &table, &typ, &col, &dtype); err != nil {
			return conn, fmt.Errorf("failed to read columns: %w", err)
		}
		if hiddenSchema(schema) {
			continue
		}
		if sch == nil || sch.Name != schema {
			conn.Schemas = append(conn.Schemas, Schema{Name: schema})
			sch, rel = &conn.Schemas[len(conn.Schemas)-1], nil
		}
		if rel == nil || rel.Name != table {
			sch.Tables = append(sch.Tables, Relation{Name: table, Kind: engine.RelationKind(typ)})
			rel = &sch.Tables[len(sch.Tables)-1]
		}
		rel.Columns = append(rel.Columns, Column{Name: col, Type: dtype})
	}
	return conn, rows.Err()
}

// builtinCatalog is DuckDB's own in-memory scratch catalog, which the hub
// keeps beside the workspace's connections once one of them is the default.
func builtinCatalog(name string) bool {
	return name == "memory" || name == "temp" || name == "system"
}

// hiddenSchema is catalog plumbing that is not data.
func hiddenSchema(s string) bool {
	return s == "information_schema" || s == "pg_catalog" || s == "pg_toast" ||
		strings.HasPrefix(s, "pg_temp_") || strings.HasPrefix(s, "pg_toast_temp_")
}

func functionNames(ctx context.Context, sess session.Session) ([]string, error) {
	rows, err := sess.DB().QueryContext(ctx,
		`SELECT DISTINCT function_name FROM duckdb_functions()
		  WHERE function_type IN ('scalar', 'aggregate', 'table', 'macro', 'table_macro')
		    AND function_name NOT LIKE '%\_\_%' ESCAPE '\'
		    AND regexp_matches(function_name, '^[a-z_][a-z0-9_]*$')
		  ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("failed to read functions: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Snapshot builds a fresh catalog from sess and merges it into the workspace's
// snapshot (SnapshotPath(root)), keeping connections that are not connected
// now. It returns how many connections it wrote; 0 means nothing was written
// (none were connected, or the save failed, and the error says which). Build errors for one connection do not stop the others; they
// are returned (with the count) after the save.
func Snapshot(ctx context.Context, sess session.Session, root string) (int, error) {
	fresh, buildErr := Build(ctx, sess)
	n := len(fresh.Connections)
	if n == 0 {
		return 0, buildErr
	}
	path, err := resolveSnapshot(root)
	if err != nil {
		return 0, err
	}
	old, err := Load(path)
	if err != nil {
		old = nil
	}
	if err := old.Merge(fresh).Save(path); err != nil {
		return 0, err
	}
	return n, buildErr
}

// Source hands the server the current snapshot, reloading the file when it
// changes (the TUI, `vibe lsp snapshot` or Studio rewrote it).
type Source struct {
	path string

	mu    sync.Mutex
	cat   *Catalog
	mtime time.Time
	size  int64
}

// NewSource reads the snapshot at path on demand.
func NewSource(path string) *Source { return &Source{path: path} }

// Catalog returns the snapshot, re-reading the file when its mtime or size
// changed. It returns nil while there is no readable snapshot.
func (s *Source) Catalog() *Catalog {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Stat(s.path)
	if err != nil {
		s.cat = nil
		return nil
	}
	if s.cat != nil && fi.ModTime().Equal(s.mtime) && fi.Size() == s.size {
		return s.cat
	}
	c, err := Load(s.path)
	if err != nil {
		return s.cat // keep the last good one while a writer is mid-change
	}
	s.cat, s.mtime, s.size = c, fi.ModTime(), fi.Size()
	return s.cat
}
