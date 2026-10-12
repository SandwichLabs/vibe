package agents

import (
	"fmt"
	"strings"
)

// The AST half of the agent SQL policy: the walk over DuckDB's serialized
// SELECT tree and the catalog/connection checks it makes. The overview is in
// sql_policy.go.

// --- the AST walk -----------------------------------------------------------

type walkState struct {
	cte     map[string]bool
	readers int
	reason  string
	summary int // inside SUMMARIZE
}

func (st *walkState) fail(format string, args ...any) {
	if st.reason == "" {
		st.reason = fmt.Sprintf(format, args...)
	}
}

// collectCTENames collects every CTE name defined anywhere in the statement.
func collectCTENames(node any, names map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if cm, ok := v["cte_map"].(map[string]any); ok {
			if arr, ok := cm["map"].([]any); ok {
				for _, item := range arr {
					if im, ok := item.(map[string]any); ok {
						if k, ok := im["key"].(string); ok && k != "" {
							names[strings.ToLower(k)] = true
						}
					}
				}
			}
		}
		for _, val := range v {
			collectCTENames(val, names)
		}
	case []any:
		for _, item := range v {
			collectCTENames(item, names)
		}
	}
}

func constString(n any) (string, bool) {
	m, ok := n.(map[string]any)
	if !ok || m["type"] != "VALUE_CONSTANT" {
		return "", false
	}
	val, ok := m["value"].(map[string]any)
	if !ok {
		return "", false
	}
	if null, _ := val["is_null"].(bool); null {
		return "", false
	}
	typ, _ := val["type"].(map[string]any)
	if typ == nil || typ["id"] != "VARCHAR" {
		return "", false
	}
	s, ok := val["value"].(string)
	return s, ok
}

// namedParamName is the lower-cased option name of a `name = value` argument.
func namedParamName(n any) string {
	m, _ := n.(map[string]any)
	l, _ := m["left"].(map[string]any)
	names, _ := l["column_names"].([]any)
	if len(names) != 1 {
		return ""
	}
	s, _ := names[0].(string)
	return strings.ToLower(s)
}

func isNamedParam(n any) bool {
	m, ok := n.(map[string]any)
	if !ok || m["type"] != "COMPARE_EQUAL" {
		return false
	}
	l, _ := m["left"].(map[string]any)
	return l != nil && l["type"] == "COLUMN_REF"
}

func (a *analyzer) walk(node any, st *walkState) {
	if st.reason != "" {
		return
	}
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			a.walk(item, st)
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		class, _ := v["class"].(string)
		switch {
		case typ == "TABLE_FUNCTION":
			if fn, ok := v["function"].(map[string]any); ok {
				a.tableFunc(fn, st)
				if st.reason != "" {
					return
				}
				for k, val := range v {
					if k == "function" {
						for fk, fv := range fn {
							if fk != "function_name" {
								a.walk(fv, st)
							}
						}
						continue
					}
					a.walk(val, st)
				}
				return
			}
		case typ == "BASE_TABLE":
			catalogName, _ := v["catalog_name"].(string)
			schema, _ := v["schema_name"].(string)
			table, _ := v["table_name"].(string)
			if catalogName == "" && schema == "" && st.cte[strings.ToLower(table)] {
				break // a CTE defined in this statement
			}
			a.checkRefParts(catalogName, schema, table, st)
		case typ == "SHOW_REF":
			a.showRef(v, st)
			if st.reason != "" {
				return
			}
			if v["show_type"] == "SUMMARY" {
				st.summary++
				for _, val := range v {
					a.walk(val, st)
				}
				st.summary--
				return
			}
		case class == "FUNCTION":
			a.scalarFunc(v, st)
		}
		for _, val := range v {
			a.walk(val, st)
		}
	}
}

func (a *analyzer) tableFunc(fn map[string]any, st *walkState) {
	name, _ := fn["function_name"].(string)
	lname := strings.ToLower(name)
	if lname == "" {
		st.fail("a table function without a name cannot be verified")
		return
	}
	if r := hardDeniedFunc(lname); r != "" {
		if lname == "query" || lname == "query_table" {
			children, _ := fn["children"].([]any)
			if len(children) == 1 {
				if s, ok := constString(children[0]); ok {
					toks := []lexTok{{Kind: lkWord, Text: strings.ToUpper(lname), Raw: lname}, {Kind: lkParen, Text: "("}, {Kind: lkString, Text: s, Plain: true}, {Kind: lkParen, Text: ")"}}
					n, r := a.literalQuery(lname, toks, 0, a.depth)
					if r != "" {
						st.fail("%s", r)
						return
					}
					st.readers += n
					return
				}
			}
			st.fail("%s() is not allowed unless its argument is one plain string literal", lname)
			return
		}
		st.fail("%s() is not allowed: %s", lname, r)
		return
	}
	switch {
	case readerFuncs[lname]:
		children, _ := fn["children"].([]any)
		positional := 0
		for _, c := range children {
			if isNamedParam(c) {
				if n := namedParamName(c); !safeReaderParams[n] {
					st.fail("%s(): the option %q is not on the agent's list of safe reader options (%s)", lname, n, safeReaderParamList())
					return
				}
				continue
			}
			positional++
			s, ok := constString(c)
			if !ok || positional > 1 {
				st.fail("%s() needs its path written out as one plain string literal (no expressions, variables, lists or concatenation)", lname)
				return
			}
			if r := a.paths.vetRead(s); r != "" {
				st.fail("%s", r)
				return
			}
			a.vetted[s] = true
		}
		if positional == 0 {
			st.fail("%s() needs a path", lname)
			return
		}
		st.readers++
	case allowedTableFunctions[lname]:
	default:
		if def, ok := a.functions().macros[lname]; ok && a.functions().tmacro[lname] {
			n, r := a.defScreen(def, a.depth+1)
			if r != "" {
				st.fail("%s", r)
				return
			}
			st.readers += n
			return
		}
		st.fail("table function %s() is not on the agent's allow-list", lname)
	}
}

func (a *analyzer) scalarFunc(v map[string]any, st *walkState) {
	name, _ := v["function_name"].(string)
	lname := strings.ToLower(name)
	if lname == "" {
		return
	}
	if r := hardDeniedFunc(lname); r != "" {
		if lname == "query" || lname == "query_table" {
			st.fail("%s() is not allowed as a scalar", lname)
			return
		}
		st.fail("%s() is not allowed: %s", lname, r)
		return
	}
	if readerFuncs[lname] {
		st.fail("%s() can only be used as a table (FROM %s('<path in data/>'))", lname, lname)
		return
	}
	if mutatingScalarFuncs[lname] {
		st.fail("%s() changes a sequence and is never classified as a read; use INSERT ... DEFAULT nextval", lname)
		return
	}
	if def, ok := a.functions().macros[lname]; ok && !a.functions().tmacro[lname] {
		n, r := a.defScreen(def, a.depth+1)
		if r != "" {
			st.fail("%s", r)
			return
		}
		st.readers += n
	}
}

// showRef allows DESCRIBE / SHOW TABLES / SUMMARIZE; the table they name is a
// BASE_TABLE inside the node and checked like any other.
func (a *analyzer) showRef(v map[string]any, st *walkState) {
	if v["query"] != nil {
		return // DESCRIBE/SUMMARIZE <table or query>
	}
	table, _ := v["table_name"].(string)
	if v["show_type"] == "SHOW_FROM" && table == "" {
		return // SHOW TABLES FROM <schema>
	}
	switch table {
	case `"tables"`, "__show_tables_expanded":
		return
	}
	st.fail("SHOW %s is not allowed (use SHOW TABLES, DESCRIBE <table> or list_connections)", strings.Trim(table, `"`))
}

// checkRefParts is checkRef over a catalog, schema and table.
func (a *analyzer) checkRefParts(catalogName, schema, table string, st *walkState) {
	a.resolveRef(catalogName, schema, table, st)
}

// checkRef checks a dotted name's parts as a table reference.
func (a *analyzer) checkRef(parts []string, st *walkState) {
	var c, s, t string
	switch len(parts) {
	case 1:
		t = parts[0]
	case 2:
		s, t = parts[0], parts[1]
	case 3:
		c, s, t = parts[0], parts[1], parts[2]
	default:
		st.fail("the table name %q has too many parts", strings.Join(parts, "."))
		return
	}
	a.resolveRef(c, s, t, st)
}

// resolveRef is the one place a table reference is checked: system objects only
// from the allow-list, views analysed, and a name that is not a table but is
// shaped like a file or URL (DuckDB would read it by replacement scan) held to
// the same path rule as read_csv('...').
func (a *analyzer) resolveRef(catalogName, schema, table string, st *walkState) {
	if a.refDenied(catalogName, schema, st) {
		return
	}
	full := dotJoin(catalogName, schema, table)
	fileish := looksLikeFile(full)
	if done := a.refHandledByCatalogName(catalogName, schema, table, fileish, st); done {
		return
	}
	cat := a.catalog()
	if e, ok := cat.lookup(catalogName, schema, table); ok {
		if e.view && e.sql != "" {
			n, r := a.defScreen(e.sql, a.depth+1)
			if r != "" {
				st.fail("%s", r)
				return
			}
			st.readers += n
		}
		return
	}
	if fileish {
		if r := a.paths.vetRead(full); r != "" {
			st.fail("%s", r)
			return
		}
		a.vetted[full] = true
		st.readers++
		return
	}
	// A qualified name inside an attached catalog that is simply not listed
	// (a table created after the attach): DuckDB reports it if it is missing.
	if catalogName != "" {
		if _, ok := a.userCatalog(catalogName); ok {
			return
		}
	}
	if catalogName == "" && schema != "" {
		if _, ok := a.userCatalog(schema); ok {
			return
		}
	}
	st.fail("%s", unresolvedTableDeniedMsg(catalogName, schema, table))
}

// userCatalog looks up an attached catalog, hiding DuckDB's own system one.
func (a *analyzer) userCatalog(n string) (dbRow, bool) {
	r, ok := a.databases()[strings.ToLower(n)]
	return r, ok && !(r.internal && strings.EqualFold(r.name, "system"))
}

// refDenied fails a reference into a remote summary, a DuckLake internal or a
// MySQL server schema.
func (a *analyzer) refDenied(catalogName, schema string, st *walkState) bool {
	switch {
	case st.summary > 0 && a.isRemote(catalogName, schema):
		st.fail("SUMMARIZE would copy the whole table out of a remote connection; use SELECT with aggregates or a LIMIT instead")
	case isDuckLakeInternal(catalogName) || isDuckLakeInternal(schema):
		st.fail("%s", duckLakeInternalMsg)
	case a.mysqlServerSchema(catalogName, schema):
		st.fail("%s", mysqlSystemSchemaMsg)
	default:
		return false
	}
	return true
}

// refHandledByCatalogName settles the references decided by the catalog part
// alone: the system catalog (allow-list) and a server's own objects.
func (a *analyzer) refHandledByCatalogName(catalogName, schema, table string, fileish bool, st *walkState) bool {
	switch {
	case strings.EqualFold(catalogName, "system"):
		if !allowedSystemObjects[systemObjectKey(schema, table)] {
			st.fail("%s", unresolvedTableDeniedMsg(catalogName, schema, table))
		}
		return true
	case catalogName != "":
		row, ok := a.userCatalog(catalogName)
		return ok && !fileish && serverSide(row) // views run on that server; DuckDB reports a missing table
	case schema != "":
		row, ok := a.userCatalog(schema)
		return ok && !fileish && serverSide(row)
	}
	return false
}

func isRemoteType(r dbRow) bool { return r.typ != "duckdb" && r.typ != "sqlite" && r.typ != "" }

// serverSide reports whether a catalog's views and queries run on its own
// server (Postgres, MySQL), so a view body there cannot read this computer's
// files. Everything else (including MotherDuck, which can run part of a query
// locally) has its view definitions analysed like a local one.
func serverSide(r dbRow) bool { return r.typ == "postgres" || r.typ == "mysql" }

// remoteCatalog reports whether a catalog is anything other than a local
// duckdb or sqlite file, by DuckDB's type or by its registered connection.
func (a *analyzer) remoteCatalog(name string) bool {
	if c, ok := a.env.conn(name); ok && c.Remote {
		return true
	}
	r, ok := a.databases()[strings.ToLower(name)]
	return ok && isRemoteType(r)
}

// isRemote reports whether a reference lands in a remote catalog: a qualified
// name, or (for unqualified names) the default catalog.
func (a *analyzer) isRemote(catalogName, schema string) bool {
	dbs := a.databases()
	name := catalogName
	if name == "" && schema != "" {
		if _, ok := dbs[strings.ToLower(schema)]; ok {
			return a.remoteCatalog(schema)
		}
	}
	if name == "" {
		name = a.catalog().defaultCatalog
	}
	return a.remoteCatalog(name)
}

// duckLakeInternalPrefix names the catalog DuckLake attaches beside every lake
// (__ducklake_metadata_<name>): the lake's own bookkeeping. Reading it exposes
// where the data files are; writing it corrupts the lake.
const duckLakeInternalPrefix = "__ducklake_metadata_"

const duckLakeInternalMsg = "a DuckLake's internal metadata catalog is not available to the agent; query the lake's own tables"

func isDuckLakeInternal(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), duckLakeInternalPrefix)
}

// mysqlSystemSchemas are the MySQL server's own databases. A MySQL catalog lists
// them beside the connection's database, and they hold account data
// (mysql.user: password hashes) and server internals. DuckDB's own
// information_schema (unqualified, or in a non-MySQL catalog) is unaffected.
var mysqlSystemSchemas = nameSet("mysql", "performance_schema", "sys", "information_schema")

const mysqlSystemSchemaMsg = "the MySQL server's own schemas (mysql, performance_schema, sys, information_schema) are not available to the agent; query the connection's database"

// mysqlServerSchema reports whether a reference lands in a system schema of a
// MySQL catalog.
func (a *analyzer) mysqlServerSchema(catalogName, schema string) bool {
	if schema == "" {
		return false
	}
	dbs := a.databases()
	cat := catalogName
	if cat == "" {
		if _, isCat := dbs[strings.ToLower(schema)]; isCat {
			return false // catalog.table: the table is in that catalog's default schema
		}
		cat = a.defaultCatalog()
	}
	r, ok := dbs[strings.ToLower(cat)]
	return ok && r.typ == "mysql" && mysqlSystemSchemas[strings.ToLower(schema)]
}

// storageConnTypes are the connection types whose Scope is a readable URL prefix.
var storageConnTypes = map[string]bool{"s3": true, "gcs": true, "r2": true, "azure": true}
