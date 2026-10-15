package connections

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// The catalog vetting (#118 step 3).
//
// DuckLake reads its own metadata with ordinary SQL against the catalog
// database: SELECT ... FROM ducklake_inlined_data_<n>_<m>, ducklake_data_file,
// and so on. A DuckDB file is a database, so a crafted one can contain a VIEW
// (or a macro) with one of those names, defined over read_text('/outside/x'):
// DuckLake would then run it as if it were its own table, and the rows (or the
// file paths PostAttach is shown) are whatever the view says. PostAttach cannot
// see this: on DuckDB 1.5 duckdb_views() and information_schema hide
// __ducklake_metadata_<name>.
//
// So before the lake is attached, the catalog file is opened on its own, under
// a throwaway alias, and every object in it is listed. It is refused unless it
// holds nothing but plain tables whose names are DuckLake's. An honest catalog
// never holds anything else: DuckLake stores its views and macros as rows
// (ducklake_view, ducklake_macro), not as catalog objects (the drift test
// asserts this against the installed extension).
//
// A catalog in object storage is read once, into a private snapshot (Stage),
// and that snapshot is what is vetted and what the lake attaches. Vetting the
// URL and then letting the lake read the URL again would trust two reads: a
// server (or anyone who can change what it returns) could answer the second
// with different bytes than the first. The lake therefore reflects the catalog
// as of the connect; a reconnect reads it again.
//
// Rows are vetted too, because object names are not the only thing DuckLake
// turns into SQL. Audit of the DuckLake 1.5.6 catalog columns, each probed with
// injection payloads against a real lake (the probe stops at "the reader bound
// it"):
//
//   - ducklake_inlined_data_tables.table_name is spliced UNQUOTED into the query
//     DuckLake runs to read inlined rows (FROM "__ducklake_metadata_x"."main".<name>),
//     so a row such as "t UNION ALL SELECT ... FROM read_text('/x') --" is a file
//     read. INJECTABLE: every name must match ducklake_inlined_data_<n>_<m> and be
//     a table found in this file. (The inlined-delete table is
//     ducklake_inlined_delete_<table_id>, derived from an integer: nothing stored.)
//   - ducklake_partition_column.transform is parsed by DuckLake against a fixed
//     set and an unknown value is an error, not SQL. It is still checked against
//     that set here (identity, year, month, day, hour, bucket(N)).
//   - Table, schema, view, macro and column names, column_type, view column
//     aliases and macro parameter names are handled as identifiers or parsed as
//     types (a quote in them stays inside the identifier; an unknown type is an
//     error). Not injectable.
//   - ducklake_column default values and ducklake_sort_expression.expression are
//     SQL by design (DEFAULT now(), SORTED BY upper(n)). They are only evaluated
//     when someone writes to the table (DuckDB refuses a subquery in a default),
//     a write needs the tier, a writable connection and approval, and old rows
//     read a stored literal as plain text.
//   - ducklake_view.sql and ducklake_macro_impl.sql are SQL by design and are
//     bound when the view or macro is used, never at attach (a view over a missing
//     function attaches fine). The agent's plan check sees what they read.
//   - Everything else is numbers, flags, stats, or paths (vetted by PostAttach).
//
// A DuckDB catalog table may also carry a DEFAULT, a generated column, a CHECK, a
// UNIQUE or a FOREIGN KEY: a generated column such as current_setting(...) would
// show configuration values as rows of a metadata table. An honest catalog has
// only PRIMARY KEY and NOT NULL, so anything else is refused (for SQLite, any
// DEFAULT, GENERATED, CHECK, UNIQUE, REFERENCES or AS in a table's CREATE
// statement).
//
// The window between this check and the lake's own ATTACH is not closed for a
// local file: it is opened twice. For a catalog on this computer, changing it in between
// needs write access to your files, which already beats everything this guards
// (the lake's data folder, the workspace itself). The scratch attachment is
// detached before the lake attaches, so the same file is never open twice at
// once.

// ducklakeMetadataTables is every table DuckLake (ducklake extension for DuckDB
// 1.5.6) creates in its catalog, apart from the per-table inlined-data tables.
// TestDuckLakeCatalogObjectsAreTheAllowList fails when an upgrade changes it.
var ducklakeMetadataTables = map[string]bool{
	"ducklake_column": true, "ducklake_column_mapping": true, "ducklake_column_tag": true,
	"ducklake_data_file": true, "ducklake_delete_file": true,
	"ducklake_file_column_stats": true, "ducklake_file_partition_value": true, "ducklake_file_variant_stats": true,
	"ducklake_files_scheduled_for_deletion": true, "ducklake_inlined_data_tables": true,
	"ducklake_macro": true, "ducklake_macro_impl": true, "ducklake_macro_parameters": true,
	"ducklake_metadata": true, "ducklake_name_mapping": true,
	"ducklake_partition_column": true, "ducklake_partition_info": true,
	"ducklake_schema": true, "ducklake_schema_versions": true,
	"ducklake_snapshot": true, "ducklake_snapshot_changes": true,
	"ducklake_sort_expression": true, "ducklake_sort_info": true,
	"ducklake_table": true, "ducklake_table_column_stats": true, "ducklake_table_stats": true,
	"ducklake_tag": true, "ducklake_view": true,
}

// ducklakeInlinedTableRE matches the tables DuckLake makes per lake table:
// ducklake_inlined_data_<table id>_<schema version> and
// ducklake_inlined_delete_<table id>.
var ducklakeInlinedTableRE = regexp.MustCompile(`^ducklake_inlined_(data_[0-9]+_[0-9]+|delete_[0-9]+)$`)

// DuckLakeCatalogTable reports whether name is a table an honest DuckLake
// catalog may hold.
func DuckLakeCatalogTable(name string) bool {
	return ducklakeMetadataTables[name] || ducklakeInlinedTableRE.MatchString(name)
}

// catalogObject is one object found in a catalog file.
type catalogObject struct {
	kind   string // table, view, macro, sequence, index, type, schema, trigger, ...
	schema string
	name   string
	sql    string // sqlite only: the object's CREATE statement
}

// catalogKind is the metadata catalog's engine for a spec: "duckdb" (the default)
// or "sqlite".
func catalogKind(s session.Spec) string {
	if opt(s, "catalog") == "sqlite" {
		return "sqlite"
	}
	return "duckdb"
}

// PreAttach refuses a catalog that holds anything but DuckLake's own tables
// (see the comment at the top of this file). A local catalog that does not
// exist yet is new: DuckLake creates it, honestly, on first write. A catalog in
// object storage has been copied to a local snapshot by Stage, and it is that
// file that is checked here.
func (ducklakeType) PreAttach(ctx context.Context, q session.QueryExecer, s session.Spec) error {
	if IsDataURL(s.Path) {
		return fmt.Errorf("the catalog in object storage was not copied before it was checked; refusing to read it")
	}
	if _, err := os.Stat(s.Path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("the lake's catalog file could not be checked: %w", err)
	}
	return VetDuckLakeCatalog(ctx, q, s.Path, catalogKind(s))
}

// VetDuckLakeCatalog opens the catalog file read-only on its own, lists every
// object and its rows that become SQL, and refuses the catalog unless it holds
// only DuckLake's tables with plain rows. kind is "duckdb" or "sqlite" (the
// sqlite extension must be loaded for the latter). The scratch attachment is
// always detached. Errors name the offending object (kind and a clipped name)
// and never contain data from the catalog.
func VetDuckLakeCatalog(ctx context.Context, q session.QueryExecer, path, kind string) error {
	var err error
	switch kind {
	case "duckdb":
		err = vetDuckDBCatalog(ctx, q, path)
	case "sqlite":
		err = vetSQLiteCatalog(ctx, q, path)
	default:
		err = fmt.Errorf("unknown catalog kind %q", kind)
	}
	return err
}

// vetFailure is a refusal (as opposed to an inability to check, which also
// refuses but says so).
func vetFailure(what string) error {
	return fmt.Errorf("the lake's catalog holds %s; refusing to read it", what)
}

func vetUnreadable(err error) error {
	return fmt.Errorf("could not check the lake's catalog before reading it, refusing it: %w", err)
}

// checkObjects refuses anything that is not a DuckLake table.
func checkObjects(objs []catalogObject) error {
	for _, o := range objs {
		if o.kind == "index" && isSQLiteAutoIndex(o) {
			continue
		}
		if o.kind != "table" || (o.schema != "" && o.schema != "main") || !DuckLakeCatalogTable(o.name) || sqliteTableHasExpression(o.sql) {
			return vetFailure(fmt.Sprintf("%s %s, which DuckLake does not create (a catalog has only its own tables)", o.kind, clip(o.name)))
		}
	}
	return nil
}

var (
	// sqliteQuotedRE matches a quoted identifier, so a column called "default"
	// is not mistaken for the keyword.
	sqliteQuotedRE = regexp.MustCompile(`"(?:[^"]|"")*"|` + "`[^`]*`" + `|\[[^\]]*\]`)
	// sqliteExpressionRE finds what a plain DuckLake table never has: defaults,
	// generated columns, checks, uniqueness and foreign keys.
	sqliteExpressionRE = regexp.MustCompile(`(?i)\b(default|generated|check|unique|references|as)\b|^\s*create\s+virtual`)
)

func sqliteTableHasExpression(createSQL string) bool {
	return sqliteExpressionRE.MatchString(sqliteQuotedRE.ReplaceAllString(createSQL, `""`))
}

var (
	inlinedNameRE = regexp.MustCompile(`^ducklake_inlined_data_[0-9]+_[0-9]+$`)
	transformRE   = regexp.MustCompile(`^(identity|year|month|day|hour|bucket\([0-9]+\))$`)
)

// rowReader reads one column of one catalog table.
type rowReader func(ctx context.Context, table, column string) ([]sql.NullString, error)

// checkRows vets the catalog rows DuckLake turns into SQL (see the audit at the
// top of this file). have is the set of tables present in the file.
func checkRows(ctx context.Context, have map[string]bool, read rowReader) error {
	if have["ducklake_inlined_data_tables"] {
		names, err := read(ctx, "ducklake_inlined_data_tables", "table_name")
		if err != nil {
			return vetUnreadable(err)
		}
		for _, n := range names {
			if !n.Valid || !inlinedNameRE.MatchString(n.String) || !have[n.String] {
				return vetFailure(fmt.Sprintf("an inlined-data entry %s that names no such table", clip(n.String)))
			}
		}
	}
	if have["ducklake_partition_column"] {
		ts, err := read(ctx, "ducklake_partition_column", "transform")
		if err != nil {
			return vetUnreadable(err)
		}
		for _, v := range ts {
			if !v.Valid || !transformRE.MatchString(v.String) {
				return vetFailure(fmt.Sprintf("a partition transform %s that DuckLake does not define", clip(v.String)))
			}
		}
	}
	return nil
}

func tableSet(objs []catalogObject) map[string]bool {
	have := map[string]bool{}
	for _, o := range objs {
		if o.kind == "table" {
			have[o.name] = true
		}
	}
	return have
}

// clip quotes a name from an untrusted file for an error message.
func clip(s string) string {
	const max = 48
	r := []rune(s)
	if len(r) > max {
		s = string(r[:max]) + "..."
	}
	return fmt.Sprintf("%q", s)
}

// isSQLiteAutoIndex reports whether o is the implicit index SQLite makes for a
// PRIMARY KEY constraint: it is named sqlite_autoindex_<table>_<n> and has no
// CREATE statement. An index someone created has one.
func isSQLiteAutoIndex(o catalogObject) bool {
	return strings.HasPrefix(o.name, "sqlite_autoindex_") && strings.TrimSpace(o.sql) == ""
}

func scanStrings(rows *sql.Rows) ([]sql.NullString, error) {
	defer rows.Close() //nolint:errcheck
	var out []sql.NullString
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// vetDuckDBCatalog attaches the file under a random alias (so no object in the
// file can be named to collide with it), vets objects and rows while it is
// attached, and detaches it.
func vetDuckDBCatalog(ctx context.Context, q session.QueryExecer, path string) (err error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return vetUnreadable(err)
	}
	alias := "__vibe_vet_" + hex.EncodeToString(b[:])
	if _, err := q.ExecContext(ctx, "ATTACH "+session.QuoteLiteral(path)+" AS "+session.QuoteIdent(alias)+" (READ_ONLY)"); err != nil {
		return vetUnreadable(fmt.Errorf("the file is not a readable DuckDB database: %w", err))
	}
	defer func() {
		// Detach even when the caller's context is cancelled.
		if _, derr := q.ExecContext(context.WithoutCancel(ctx), "DETACH "+session.QuoteIdent(alias)); derr != nil && err == nil {
			err = vetUnreadable(fmt.Errorf("could not detach the catalog after checking it: %w", derr))
		}
	}()
	objs, err := listDuckDBCatalog(ctx, q, alias)
	if err != nil {
		return vetUnreadable(err)
	}
	if err := checkObjects(objs); err != nil {
		return err
	}
	return checkRows(ctx, tableSet(objs), func(ctx context.Context, table, column string) ([]sql.NullString, error) {
		rows, err := q.QueryContext(ctx, "SELECT "+session.QuoteIdent(column)+" FROM "+session.QuoteIdent(alias)+".main."+session.QuoteIdent(table))
		if err != nil {
			return nil, err
		}
		return scanStrings(rows)
	})
}

// listDuckDBCatalog lists tables, views, macros, sequences, indexes, types,
// schemas, and what a table carries beyond PRIMARY KEY and NOT NULL.
func listDuckDBCatalog(ctx context.Context, q session.QueryExecer, alias string) ([]catalogObject, error) {
	const query = `
		SELECT 'table', schema_name, table_name FROM duckdb_tables() WHERE database_name = $1
		UNION ALL SELECT 'view', schema_name, view_name FROM duckdb_views() WHERE database_name = $1
		UNION ALL SELECT 'macro', schema_name, function_name FROM duckdb_functions() WHERE database_name = $1
		UNION ALL SELECT 'sequence', schema_name, sequence_name FROM duckdb_sequences() WHERE database_name = $1
		UNION ALL SELECT 'index', schema_name, index_name FROM duckdb_indexes() WHERE database_name = $1
		UNION ALL SELECT 'type', schema_name, type_name FROM duckdb_types() WHERE database_name = $1 AND NOT internal
		UNION ALL SELECT 'schema', schema_name, schema_name FROM duckdb_schemas() WHERE database_name = $1 AND schema_name <> 'main'
		UNION ALL SELECT 'default or generated column', schema_name, table_name FROM duckdb_columns() WHERE database_name = $1 AND column_default IS NOT NULL
		UNION ALL SELECT 'constraint', schema_name, table_name FROM duckdb_constraints() WHERE database_name = $1 AND constraint_type NOT IN ('PRIMARY KEY', 'NOT NULL')`
	rows, err := q.QueryContext(ctx, query, alias)
	if err != nil {
		return nil, fmt.Errorf("listing the catalog's objects: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var objs []catalogObject
	for rows.Next() {
		var o catalogObject
		if err := rows.Scan(&o.kind, &o.schema, &o.name); err != nil {
			return nil, fmt.Errorf("listing the catalog's objects: %w", err)
		}
		objs = append(objs, o)
	}
	return objs, rows.Err()
}

// vetSQLiteCatalog reads the file's own sqlite_master through sqlite_scan,
// which lists tables, views, indexes and triggers alike (an attached SQLite
// database only exposes tables and views to duckdb_*()), then its rows the same
// way. The file is opened read-only by the sqlite extension and nothing is
// attached.
func vetSQLiteCatalog(ctx context.Context, q session.QueryExecer, path string) error {
	lit := session.QuoteLiteral(path)
	rows, err := q.QueryContext(ctx, `SELECT type, name, coalesce(sql, '') FROM sqlite_scan(`+lit+`, 'sqlite_master')`)
	if err != nil {
		return vetUnreadable(fmt.Errorf("the file is not a readable SQLite database: %w", err))
	}
	var objs []catalogObject
	for rows.Next() {
		var o catalogObject
		if err := rows.Scan(&o.kind, &o.name, &o.sql); err != nil {
			_ = rows.Close()
			return vetUnreadable(err)
		}
		objs = append(objs, o)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return vetUnreadable(err)
	}
	_ = rows.Close()
	if err := checkObjects(objs); err != nil {
		return err
	}
	return checkRows(ctx, tableSet(objs), func(ctx context.Context, table, column string) ([]sql.NullString, error) {
		r, err := q.QueryContext(ctx, "SELECT "+session.QuoteIdent(column)+" FROM sqlite_scan("+lit+", "+session.QuoteLiteral(table)+")")
		if err != nil {
			return nil, err
		}
		return scanStrings(r)
	})
}
