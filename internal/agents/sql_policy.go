package agents

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// This file is the agent SQL policy (#116, plan §4.5). It inverts the old
// deny-list into an allow-list per tier and runs in layers, cheapest and most
// side-effect-free first:
//
//  1. Statement count and lexing. DuckDB's duckdb_extract_statements is the
//     authority on how many statements there are; the policy's own lexer must
//     agree, or the query is denied.
//  2. A syntactic check BEFORE any EXPLAIN (spike #107 finding B: binding
//     sniffs files and sends HTTP requests, so the classifier's own EXPLAIN
//     could exfiltrate). SELECT-shaped statements are parsed by DuckDB
//     (json_serialize_sql, on a separate hardened connection with external
//     access off) and the AST is walked; everything else is checked on its
//     tokens. Both enforce the never-list, the table-function allow-list and
//     the literal-path rule.
//  3. Only then EXPLAIN (FORMAT JSON) on the real connection, and a plan-
//     operator allow-list: the second layer, which sees through views and
//     macros that wrap a denied function.
//
// Writes also need the target connection to be writable and the person to
// approve; those two gates live in Handler.gateWrite (this file only reports
// the targets).

// sqlEnv is what the policy needs to know about the workspace. The zero value
// (and nil) means no data/ folder, no storage scopes and no connection
// registry: no file or URL read classifies as a read.
type sqlEnv struct {
	dataDir string                   // absolute, symlink-resolved; "" = none
	conns   []session.ConnectionInfo // registered connections
	// protected: files and folders of attached connections (symlink-resolved);
	// COPY ... TO never writes into them.
	protected []string
}

// newSQLEnv builds the environment from the workspace's data/ folder (may be
// "") and its registered connections.
func newSQLEnv(dataDir string, conns []session.ConnectionInfo, protected ...string) *sqlEnv {
	e := &sqlEnv{conns: conns}
	for _, p := range protected {
		if p == "" {
			continue
		}
		if real, err := realPath(p); err == nil {
			e.protected = append(e.protected, real)
		}
	}
	if dataDir != "" {
		if abs, err := filepath.Abs(dataDir); err == nil {
			if real, err := filepath.EvalSymlinks(abs); err == nil {
				e.dataDir = real
			}
		}
	}
	return e
}

func (e *sqlEnv) conn(name string) (session.ConnectionInfo, bool) {
	if e == nil {
		return session.ConnectionInfo{}, false
	}
	for _, c := range e.conns {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return session.ConnectionInfo{}, false
}

func (e *sqlEnv) pathPolicy() *pathPolicy {
	p := &pathPolicy{}
	if e == nil {
		return p
	}
	p.dataDir = e.dataDir
	p.protected = e.protected
	for _, c := range e.conns {
		// Only a storage connection widens what is readable (defence in depth:
		// the session exports a scope for no other type).
		if !c.Catalogless || c.Scope == "" || !storageConnTypes[c.Type] {
			continue
		}
		// A storage connection is ready when it is connected or can connect;
		// one that needs credentials or failed does not widen what is readable.
		if c.Kind != session.StatusOK && c.Kind != session.StatusNotConnected {
			continue
		}
		if s := normalizeScope(c.Scope); s != "" {
			p.scopes = append(p.scopes, s)
		}
	}
	return p
}

// analyzeSQL classifies raw (the query text with $name placeholders) plus
// params. Everything downstream classifies the BOUND form: placeholders become
// positional "?" first, so what is classified is what runs.
func analyzeSQL(ctx context.Context, userDB queryDB, env *sqlEnv, raw string, params map[string]any) *sqlVerdict {
	if touchesSecrets(raw) {
		return deny(secretsDeniedMsg)
	}
	bound, args, err := engine.BindParams(raw, params)
	if err != nil {
		return deny(fmt.Sprintf("could not validate query: %v", err))
	}
	a := &analyzer{ctx: ctx, db: userDB, env: env, paths: env.pathPolicy(), vetted: map[string]bool{}}
	return a.script(bound, args)
}

type analyzer struct {
	ctx   context.Context
	db    queryDB
	env   *sqlEnv
	paths *pathPolicy
	src   string // the source the tokens being checked index into
	// vetted holds the file and URL literals the path policy accepted. The
	// string net (stringNet) lets only these, and inert strings, reach EXPLAIN.
	vetted map[string]bool
	depth  int // nesting of views, macros and query('...')

	cat    *catalog
	catErr error
	dbs    map[string]dbRow
	dbsErr error
	funcs  *funcInfo
	fnErr  error
}

func (a *analyzer) catalog() *catalog {
	if a.cat == nil && a.catErr == nil {
		a.cat, a.catErr = loadCatalog(a.ctx, a.db)
		if a.cat == nil {
			a.cat = &catalog{}
		}
	}
	return a.cat
}

func (a *analyzer) databases() map[string]dbRow {
	if a.dbs == nil && a.dbsErr == nil {
		a.dbs, a.dbsErr = loadDatabases(a.ctx, a.db)
	}
	return a.dbs
}

func (a *analyzer) functions() *funcInfo {
	if a.funcs == nil && a.fnErr == nil {
		a.funcs, a.fnErr = loadFunctions(a.ctx, a.db)
		if a.funcs == nil {
			a.funcs = &funcInfo{table: map[string]bool{}, macros: map[string]string{}, tmacro: map[string]bool{}}
		}
	}
	return a.funcs
}

// defaultCatalog is the catalog unqualified names resolve against.
func (a *analyzer) defaultCatalog() string { return strings.ToLower(a.catalog().defaultCatalog) }

// --- scripts and statements -------------------------------------------------

// script checks every statement of a (bound) script. The verdict is the
// highest class among them with all targets; the first denial wins.
func (a *analyzer) script(bound string, args []any) *sqlVerdict {
	stmts := lexStatements(bound)
	if len(stmts) == 0 {
		return deny("the statement is empty")
	}
	// Heads first, before DuckDB's parser sees the text: some statements do
	// work at parse time (IMPORT DATABASE reads its schema file), and the
	// never-list needs no parser to be refused.
	for _, toks := range stmts {
		if v := preScreen(toks); v != nil {
			return v
		}
	}
	n, err := countStatements(bound)
	if err != nil {
		return deny(fmt.Sprintf("%s: %s", couldntVerifyMsg, firstLine(err.Error())))
	}
	if n != len(stmts) {
		for _, toks := range stmts {
			for _, t := range toks {
				if t.isWord("PIVOT") || t.isWord("UNPIVOT") {
					return deny(fmt.Sprintf("%s: the PIVOT/UNPIVOT statement form expands into several statements inside DuckDB; use FROM <table> PIVOT (... IN (...)) or a reader on a path inside data/", couldntVerifyMsg))
				}
			}
		}
		return deny(fmt.Sprintf("%s: it could not be split into statements reliably", couldntVerifyMsg))
	}
	a.src = bound
	out := &sqlVerdict{Class: sqlClassRead}
	argPos := 0
	for _, toks := range stmts {
		nq := 0
		for _, t := range toks {
			if t.isPunct("?") {
				nq++
			}
		}
		if argPos+nq > len(args) {
			return deny(fmt.Sprintf("%s: parameters do not line up with the statement", couldntVerifyMsg))
		}
		stmtArgs := args[argPos : argPos+nq]
		argPos += nq
		v := a.statement(toks, stmtArgs, 0)
		if v.Class == sqlClassOther {
			return v
		}
		if v.Class > out.Class {
			out.Class = v.Class
		}
		out.Targets = append(out.Targets, v.Targets...)
	}
	if argPos != len(args) {
		return deny(fmt.Sprintf("%s: parameters do not line up with the statement", couldntVerifyMsg))
	}
	return out
}

// preScreen refuses the never-list statement kinds by their first token.
func preScreen(toks []lexTok) *sqlVerdict {
	// U&'...' and U&"..." decode escapes the lexer does not, so a name or a
	// path could be spelled in a way no token check recognises.
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].isWord("U") && toks[i+1].isPunct("&") && toks[i].End == toks[i+1].Start && toks[i+1].End == toks[i+2].Start &&
			(toks[i+2].Kind == lkString || toks[i+2].Kind == lkIdent) {
			return deny("Unicode escape strings and identifiers (U&'...') are not allowed")
		}
	}
	head := toks[0]
	if head.Kind == lkWord {
		if why, ok := deniedHeads[head.Text]; ok {
			return deny(why)
		}
		if head.Text == "COPY" {
			depth := 0
			for _, t := range toks[1:] {
				switch {
				case t.isParen("("):
					depth++
				case t.isParen(")"):
					depth--
				case depth == 0 && t.isWord("FROM"):
					return deny("COPY ... FROM is not allowed (COPY FROM DATABASE and file imports): load a file with INSERT INTO ... SELECT * FROM read_csv('<path in data/>')")
				}
			}
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var readHeads = nameSet("SELECT", "WITH", "FROM", "VALUES", "TABLE", "DESCRIBE", "SHOW", "SUMMARIZE")

// textOf is the source text of a statement's tokens.
func (a *analyzer) textOf(toks []lexTok) string { return a.src[toks[0].Start:toks[len(toks)-1].End] }

// statement classifies one statement (toks is its token stream into a.src).
// depth guards the recursion of EXPLAIN and query('...').
func (a *analyzer) statement(toks []lexTok, args []any, depth int) *sqlVerdict {
	if depth > 3 {
		return deny(fmt.Sprintf("%s: statements nest too deeply", couldntVerifyMsg))
	}
	head := toks[0]
	if head.Kind == lkWord {
		if why, ok := deniedHeads[head.Text]; ok {
			return deny(why)
		}
	}
	switch {
	case head.isWord("EXPLAIN"):
		return a.explain(toks, args, depth)
	case head.isWord("PRAGMA"):
		return a.pragma(toks)
	case head.isWord("CALL"):
		return a.call(toks)
	case head.isParen("(") || (head.Kind == lkWord && readHeads[head.Text]):
		return a.readOrWith(toks, args)
	case head.isWord("INSERT"), head.isWord("UPDATE"), head.isWord("DELETE"), head.isWord("MERGE"):
		return a.dml(toks, args)
	case head.isWord("CREATE"), head.isWord("DROP"), head.isWord("ALTER"):
		return a.ddl(toks, args)
	case head.isWord("COPY"):
		return a.copyStmt(toks, args)
	}
	return deny(fmt.Sprintf("this kind of statement (%s) is not on the agent's allow-list", head.Raw))
}

// readOrWith handles every statement DuckDB can serialize (a pure SELECT
// batch), and WITH ... INSERT/UPDATE/DELETE which it cannot.
func (a *analyzer) readOrWith(toks []lexTok, args []any) *sqlVerdict {
	text := a.textOf(toks)
	doc, err := serializeSQL(a.ctx, text)
	if err != nil {
		return deny(fmt.Sprintf("%s: %s", couldntVerifyMsg, firstLine(err.Error())))
	}
	if !doc.Error && len(doc.Statements) == 1 {
		return a.read(doc.Statements[0], toks, text, args)
	}
	if doc.Error && toks[0].isWord("WITH") && doc.ErrorType == "not implemented" {
		return a.dml(toks, args)
	}
	return deny(fmt.Sprintf("%s: it did not parse as a single read statement", couldntVerifyMsg))
}

// read checks a parsed SELECT-shaped statement: the token screen, the AST walk,
// then the plan.
func (a *analyzer) read(stmt map[string]any, toks []lexTok, text string, args []any) *sqlVerdict {
	if _, reason := a.screenTokens(toks, true, 0); reason != "" {
		return deny(reason)
	}
	w := &walkState{cte: map[string]bool{}}
	collectCTENames(stmt, w.cte)
	a.walk(stmt, w)
	if w.reason != "" {
		return deny(w.reason)
	}
	// No string net here: the AST is authoritative for sources (BASE_TABLE and
	// TABLE_FUNCTION nodes; a VALUE_CONSTANT is never a source), so a read-only
	// query may carry path- or URL-looking values.
	if r := planCheck(a.ctx, a.db, text, args, w.readers); r != "" {
		return deny(r)
	}
	return &sqlVerdict{Class: sqlClassRead, readers: w.readers}
}

// explain allows EXPLAIN of a read, never EXPLAIN ANALYZE (it executes the
// statement: INSERT, CTAS and COPY TO all ran under it, spike finding A).
func (a *analyzer) explain(toks []lexTok, args []any, depth int) *sqlVerdict {
	i := 1
	if i < len(toks) && toks[i].isParen("(") {
		// Only EXPLAIN (FORMAT <word>) is accepted.
		if !(i+3 < len(toks) && toks[i+1].isWord("FORMAT") && toks[i+2].Kind == lkWord && toks[i+3].isParen(")")) {
			return deny("EXPLAIN options other than (FORMAT ...) are not allowed (EXPLAIN ANALYZE executes the statement)")
		}
		i += 4
	}
	if i < len(toks) && toks[i].isWord("ANALYZE") {
		return deny("EXPLAIN ANALYZE is not allowed: it executes the statement")
	}
	if i >= len(toks) {
		return deny("EXPLAIN needs a statement")
	}
	v := a.statement(toks[i:], args, depth+1)
	if v.Class == sqlClassOther {
		return v
	}
	if v.Class != sqlClassRead {
		return deny("EXPLAIN is only allowed over a read statement")
	}
	return &sqlVerdict{Class: sqlClassRead, readers: v.readers}
}

// literalArgs reports whether toks (the inside of a call's parentheses) are
// only plain string literals, numbers, true/false/null and separators.
func literalArgs(toks []lexTok) bool {
	for _, t := range toks {
		switch {
		case t.Kind == lkString && t.Plain:
		case t.Kind == lkPunct && (t.Text == "," || t.Text == "-" || t.Text == "."):
		case t.Kind == lkWord && (t.Text == "TRUE" || t.Text == "FALSE" || t.Text == "NULL" || (t.Text != "" && t.Text[0] >= '0' && t.Text[0] <= '9')):
		default:
			return false
		}
	}
	return true
}

// pragma allows only the read-style introspection PRAGMAs.
func (a *analyzer) pragma(toks []lexTok) *sqlVerdict {
	if len(toks) < 2 || !toks[1].isName() {
		return deny("PRAGMA needs a name")
	}
	name := strings.ToLower(nameOf(toks[1]))
	if !allowedPragmas[name] {
		return deny(fmt.Sprintf("PRAGMA %s is not on the allow-list (only %s)", name, sortedNames(allowedPragmas)))
	}
	if len(toks) > 2 {
		if !toks[2].isParen("(") || matchParen(toks, 2) != len(toks)-1 || !literalArgs(toks[3:len(toks)-1]) {
			return deny("a PRAGMA may only take literal arguments")
		}
	}
	return &sqlVerdict{Class: sqlClassRead}
}

// call allows only CALL of the catalog metadata functions with literal arguments.
func (a *analyzer) call(toks []lexTok) *sqlVerdict {
	if len(toks) < 4 || !toks[1].isName() || !toks[2].isParen("(") || matchParen(toks, 2) != len(toks)-1 {
		return deny("CALL is only allowed as CALL <catalog function>(literal arguments)")
	}
	name := strings.ToLower(nameOf(toks[1]))
	if !allowedCalls[name] {
		return deny(fmt.Sprintf("CALL %s is not on the allow-list (only %s)", name, sortedNames(allowedCalls)))
	}
	if !literalArgs(toks[3 : len(toks)-1]) {
		return deny("CALL may only take literal arguments")
	}
	return &sqlVerdict{Class: sqlClassRead}
}

func nameOf(t lexTok) string {
	if t.Kind == lkIdent {
		return t.Text
	}
	return t.Raw
}

func sortedNames(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// small, fixed sets: a simple insertion sort keeps messages stable.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return strings.Join(out, ", ")
}

// --- DML, DDL and COPY ------------------------------------------------------

// parseName reads a dotted name starting at toks[i]: a, a.b or a.b.c, each part
// a bare word or a quoted identifier.
func parseName(toks []lexTok, i int) (parts []string, next int, ok bool) {
	for {
		if i >= len(toks) || !toks[i].isName() {
			return nil, i, false
		}
		parts = append(parts, nameOf(toks[i]))
		i++
		if i < len(toks) && toks[i].isPunct(".") {
			i++
			continue
		}
		return parts, i, len(parts) <= 3
	}
}

// dml checks INSERT / UPDATE / DELETE / MERGE (and WITH ... of them): the token
// screen, the target catalogs, then the plan.
func (a *analyzer) dml(toks []lexTok, args []any) *sqlVerdict {
	start := 0
	if toks[0].isWord("WITH") {
		start = -1
		depth := 0
		for i, t := range toks {
			switch {
			case t.isParen("("):
				depth++
			case t.isParen(")"):
				depth--
			case depth == 0 && (t.isWord("INSERT") || t.isWord("UPDATE") || t.isWord("DELETE") || t.isWord("MERGE")):
				start = i
			}
			if start >= 0 {
				break
			}
		}
		if start < 0 {
			return deny(fmt.Sprintf("%s: it is not a statement the policy recognises", couldntVerifyMsg))
		}
	}
	if toks[start].Text == "UPDATE" {
		// UPDATE EXTENSIONS downloads extensions; a row UPDATE always has SET.
		hasSet := false
		depth := 0
		for _, t := range toks[start:] {
			switch {
			case t.isParen("("):
				depth++
			case t.isParen(")"):
				depth--
			case depth == 0 && t.isWord("SET"):
				hasSet = true
			}
		}
		if !hasSet {
			return deny("UPDATE is only allowed as UPDATE <table> SET ... (UPDATE EXTENSIONS is not allowed)")
		}
	}
	nameAt := -1
	switch toks[start].Text {
	case "INSERT":
		for i := start + 1; i < len(toks) && i <= start+4; i++ {
			if toks[i].isWord("INTO") {
				nameAt = i + 1
				break
			}
		}
	case "UPDATE":
		nameAt = start + 1
	case "DELETE":
		if start+1 < len(toks) && toks[start+1].isWord("FROM") {
			nameAt = start + 2
		}
	case "MERGE":
		if start+1 < len(toks) && toks[start+1].isWord("INTO") {
			nameAt = start + 2
		}
	}
	if nameAt < 0 {
		return deny(fmt.Sprintf("%s: could not tell which table this statement changes", couldntVerifyMsg))
	}
	parts, _, ok := parseName(toks, nameAt)
	if !ok {
		return deny(fmt.Sprintf("%s: could not tell which table this statement changes", couldntVerifyMsg))
	}
	targets, reason := a.targetsFor(parts, false, false)
	if reason != "" {
		return deny(reason)
	}
	readers, reason := a.screenTokens(toks, false, 0)
	if reason != "" {
		return deny(reason)
	}
	if r := a.stringNet(toks); r != "" {
		return deny(r)
	}
	// EXPLAIN cannot even be run against a read-only database; the write gate
	// refuses such a target, so there is nothing to bind.
	if !anyReadOnly(targets) {
		if r := planCheck(a.ctx, a.db, a.textOf(toks), args, readers); r != "" {
			return deny(r)
		}
	}
	return &sqlVerdict{Class: sqlClassWrite, Targets: targets}
}

var ddlKinds = nameSet("TABLE", "VIEW", "SEQUENCE", "SCHEMA", "INDEX", "MACRO", "FUNCTION", "TYPE")

// ddl checks CREATE / DROP / ALTER of tables, views, sequences, schemas,
// indexes, macros and types. CREATE/DROP DATABASE, SHARE and SECRET and
// everything else are refused.
func (a *analyzer) ddl(toks []lexTok, args []any) *sqlVerdict {
	verb := toks[0].Text
	i := 1
	temp := false
	for i < len(toks) {
		w := toks[i]
		if !(w.isWord("OR") || w.isWord("REPLACE") || w.isWord("TEMP") || w.isWord("TEMPORARY") || w.isWord("UNIQUE")) {
			break
		}
		if w.isWord("TEMP") || w.isWord("TEMPORARY") {
			temp = true
		}
		i++
	}
	if i >= len(toks) || toks[i].Kind != lkWord || !ddlKinds[toks[i].Text] {
		what := ""
		if i < len(toks) {
			what = toks[i].Raw
		}
		return deny(fmt.Sprintf("%s %s is not allowed (the agent may %s tables, views, sequences, schemas, indexes, macros and types)", verb, what, strings.ToLower(verb)))
	}
	kind := toks[i].Text
	if verb == "ALTER" && kind != "TABLE" && kind != "VIEW" && kind != "SEQUENCE" {
		return deny("ALTER " + kind + " is not allowed")
	}
	i++
	// IF [NOT] EXISTS
	if i < len(toks) && toks[i].isWord("IF") {
		i++
		if i < len(toks) && toks[i].isWord("NOT") {
			i++
		}
		if i < len(toks) && toks[i].isWord("EXISTS") {
			i++
		}
	}
	var targets []sqlTarget
	addTargets := func(parts []string, schemaKind bool) string {
		ts, reason := a.targetsFor(parts, schemaKind, temp)
		targets = append(targets, ts...)
		return reason
	}
	switch {
	case verb == "CREATE" && kind == "INDEX":
		// CREATE [UNIQUE] INDEX [IF NOT EXISTS] [name] ON table (...)
		on := -1
		for j := i; j < len(toks); j++ {
			if toks[j].isWord("ON") {
				on = j
				break
			}
		}
		if on < 0 {
			return deny(fmt.Sprintf("%s: could not tell which table this index is on", couldntVerifyMsg))
		}
		parts, _, ok := parseName(toks, on+1)
		if !ok {
			return deny(fmt.Sprintf("%s: could not tell which table this index is on", couldntVerifyMsg))
		}
		if r := addTargets(parts, false); r != "" {
			return deny(r)
		}
	default:
		parts, _, ok := parseName(toks, i)
		if !ok {
			return deny(fmt.Sprintf("%s: could not tell which object this statement changes", couldntVerifyMsg))
		}
		if r := addTargets(parts, kind == "SCHEMA"); r != "" {
			return deny(r)
		}
	}
	readers, reason := a.screenTokens(toks, false, 0)
	if reason != "" {
		return deny(reason)
	}
	if r := a.stringNet(toks); r != "" {
		return deny(r)
	}
	if verb == "CREATE" && hasQueryPart(toks) && !anyReadOnly(targets) {
		if r := planCheck(a.ctx, a.db, a.textOf(toks), args, readers); r != "" {
			return deny(r)
		}
	}
	return &sqlVerdict{Class: sqlClassDDL, Targets: targets}
}

func anyReadOnly(targets []sqlTarget) bool {
	for _, t := range targets {
		if t.ReadOnly {
			return true
		}
	}
	return false
}

// hasQueryPart reports whether a CREATE statement has a query in it (CTAS, a
// view, a macro body), so it is worth a plan check.
func hasQueryPart(toks []lexTok) bool {
	for _, t := range toks {
		if t.isWord("SELECT") || t.isWord("FROM") || t.isWord("VALUES") || t.isWord("WITH") || t.isWord("PIVOT") || t.isWord("UNPIVOT") {
			return true
		}
	}
	return false
}

// copyStmt allows only COPY <table | (query)> TO '<literal path in data/>' [options].
// COPY ... FROM (a file or a database) is refused: load a file with
// INSERT ... SELECT * FROM read_csv('<path in data/>') instead.
func (a *analyzer) copyStmt(toks []lexTok, args []any) *sqlVerdict {
	depth, to := 0, -1
	for i := 1; i < len(toks); i++ {
		switch {
		case toks[i].isParen("("):
			depth++
		case toks[i].isParen(")"):
			depth--
		case depth == 0 && toks[i].isWord("FROM"):
			return deny("COPY ... FROM is not allowed (COPY FROM DATABASE and file imports): load a file with INSERT INTO ... SELECT * FROM read_csv('<path in data/>')")
		case depth == 0 && toks[i].isWord("TO") && to < 0:
			to = i
		}
	}
	if to < 0 || to+1 >= len(toks) || toks[to+1].Kind != lkString || !toks[to+1].Plain {
		return deny("COPY must be COPY <table or (query)> TO '<path>' with the path written as a plain string literal")
	}
	if r := a.paths.vetWrite(toks[to+1].Text); r != "" {
		return deny(r)
	}
	a.vetted[toks[to+1].Text] = true
	// Options after the path are a parenthesised list (or WITH (...)): nothing else.
	rest := toks[to+2:]
	if len(rest) > 0 && rest[0].isWord("WITH") {
		rest = rest[1:]
	}
	if len(rest) > 0 && !(rest[0].isParen("(") && matchParen(rest, 0) == len(rest)-1) {
		return deny("COPY ... TO takes only a parenthesised option list after the path")
	}
	if toks[1].Kind != lkParen {
		parts, _, ok := parseName(toks, 1)
		if !ok {
			return deny(fmt.Sprintf("%s: could not tell what COPY reads", couldntVerifyMsg))
		}
		st := &walkState{cte: map[string]bool{}}
		a.checkRef(parts, st)
		if st.reason != "" {
			return deny(st.reason)
		}
	}
	readers, reason := a.screenTokens(toks[:to], false, 0)
	if reason != "" {
		return deny(reason)
	}
	if r := a.stringNet(toks); r != "" {
		return deny(r)
	}
	if r := planCheck(a.ctx, a.db, a.textOf(toks), args, readers); r != "" {
		return deny(r)
	}
	return &sqlVerdict{Class: sqlClassDDL}
}

// targetsFor names the catalogs a write to parts changes. DuckDB resolves a
// two-part name to a schema of the default catalog first and a catalog second,
// so when the first part names a catalog both are targets and both must pass
// the gates.
func (a *analyzer) targetsFor(parts []string, isSchema, temp bool) ([]sqlTarget, string) {
	dbs := a.databases()
	if a.dbsErr != nil || len(dbs) == 0 {
		return nil, couldntVerifyMsg + ": could not read the list of connections"
	}
	var names []string
	switch {
	case temp:
		names = []string{"temp"}
	case len(parts) == 3:
		names = []string{parts[0]}
	case len(parts) == 2 && isSchema:
		names = []string{parts[0]}
	case len(parts) == 2:
		names = []string{a.defaultCatalog()}
		if _, ok := dbs[strings.ToLower(parts[0])]; ok {
			names = append(names, parts[0])
		}
	case len(parts) == 1:
		names = []string{a.defaultCatalog()}
	default:
		return nil, couldntVerifyMsg + ": the name has too many parts"
	}
	var out []sqlTarget
	seen := map[string]bool{}
	for _, n := range names {
		ln := strings.ToLower(n)
		if seen[ln] {
			continue
		}
		seen[ln] = true
		if isDuckLakeInternal(ln) {
			return nil, duckLakeInternalMsg
		}
		row, ok := dbs[ln]
		if !ok {
			return nil, fmt.Sprintf("there is no connection named %q (see list_connections)", n)
		}
		if ln == "system" {
			return nil, "DuckDB's system catalog cannot be changed"
		}
		conn, registered := a.env.conn(ln)
		t := sqlTarget{Catalog: ln}
		t.ReadOnly = row.readOnly || (registered && conn.ReadOnly)
		t.Local = (row.typ == "duckdb" || row.typ == "sqlite") && !(registered && conn.Remote)
		out = append(out, t)
	}
	return out, ""
}

// --- the string net ---

var urlishRE = regexp.MustCompile(`^([A-Za-z]:[\\/]|[A-Za-z][A-Za-z0-9+.\-]*:/)`)

// inertString reports whether a string literal cannot be taken for a file or
// URL: no scheme, not shaped like a path, no data-file extension.
func inertString(s string) bool {
	t := strings.TrimSpace(s)
	switch {
	case strings.Contains(t, "://"), urlishRE.MatchString(t):
		return false
	case strings.HasPrefix(t, "/"), strings.HasPrefix(t, "~"), strings.HasPrefix(t, "./"), strings.HasPrefix(t, "../"), strings.HasPrefix(t, `\`):
		return false
	}
	lower := strings.ToLower(t)
	for _, e := range replacementScanExts {
		if strings.HasSuffix(lower, e) {
			return false
		}
	}
	return true
}

// stringNet is the safety net that does not depend on knowing every place SQL
// can name a table source (FROM, JOIN, PIVOT, UNPIVOT, TABLE, ...): before any
// EXPLAIN binds a statement, every string literal and quoted identifier in it
// must be either a literal the path policy already accepted as a reader path
// or URL, or inert (it cannot be read as a file or URL). It is blunt on
// purpose: a data value such as '/etc/passwd' or 'report.csv' is refused too.
func (a *analyzer) stringNet(toks []lexTok) string {
	for _, t := range toks {
		if (t.Kind == lkString || t.Kind == lkIdent) && !a.vetted[t.Text] && !inertString(t.Text) {
			return "a file or URL here can't be checked; use a reader function (read_csv('<path>') ...) on a path inside data/"
		}
	}
	return ""
}

// --- the token screen --------------------------------------------------------

// cteNamesFromTokens collects the names a statement defines with WITH.
func cteNamesFromTokens(toks []lexTok) map[string]bool {
	names := map[string]bool{}
	for j, t := range toks {
		if !t.isWord("AS") {
			continue
		}
		k := j + 1
		for k < len(toks) && (toks[k].isWord("NOT") || toks[k].isWord("MATERIALIZED")) {
			k++
		}
		if k >= len(toks) || !toks[k].isParen("(") {
			continue
		}
		p := j - 1
		if p >= 0 && toks[p].isParen(")") { // a column list: WITH a(x, y) AS (...)
			depth := 0
			for ; p >= 0; p-- {
				if toks[p].isParen(")") {
					depth++
				} else if toks[p].isParen("(") {
					depth--
					if depth == 0 {
						p--
						break
					}
				}
			}
		}
		if p >= 0 && toks[p].isName() {
			names[strings.ToLower(nameOf(toks[p]))] = true
		}
	}
	return names
}

// screenTokens applies the policy to a statement's tokens, whatever its shape.
// astMode means an AST walk covers table functions and table references (a
// SELECT-shaped statement), so only the never-list and the literal-path rule
// are applied here; otherwise (DML, DDL, COPY, view and macro bodies) it also
// checks table functions against the allow-list and every table reference.
// It returns how many readers were vetted.
func (a *analyzer) screenTokens(toks []lexTok, astMode bool, depth int) (readers int, reason string) {
	if depth > 4 {
		return 0, "views and macros nest too deeply to verify"
	}
	var cte map[string]bool
	for i, t := range toks {
		if t.isName() && i+1 < len(toks) && toks[i+1].isParen("(") {
			name := strings.ToLower(nameOf(t))
			if r := hardDeniedFunc(name); r != "" {
				if name == "query" || name == "query_table" {
					// A single literal argument is checked recursively.
					n, rr := a.literalQuery(name, toks, i, depth)
					if rr != "" {
						return 0, rr
					}
					if !astMode {
						readers += n
					}
					continue
				}
				return 0, fmt.Sprintf("%s() is not allowed: %s", name, r)
			}
			switch {
			case readerFuncs[name]:
				if r := a.vetReaderTokens(name, toks, i+1); r != "" {
					return 0, r
				}
				if !astMode {
					readers++
				}
			case !astMode:
				fi := a.functions()
				if def, isMacro := fi.macros[name]; isMacro {
					n, r := a.defScreen(def, depth+1)
					if r != "" {
						return 0, r
					}
					readers += n
				} else if fi.table[name] && !allowedTableFunctions[name] {
					return 0, fmt.Sprintf("table function %s() is not on the agent's allow-list", name)
				}
			}
		}
		if astMode {
			continue
		}
		if cte == nil {
			cte = cteNamesFromTokens(toks)
		}
		if isTableRefStart(toks, i) {
			parts, next, ok := tableRefAt(toks, i)
			if !ok || (next < len(toks) && toks[next].isParen("(")) {
				continue // a function call or a subquery, not a table name
			}
			if len(parts) == 1 && cte[strings.ToLower(parts[0])] {
				continue
			}
			st := &walkState{cte: cte}
			a.checkRef(parts, st)
			if st.reason != "" {
				return 0, st.reason
			}
			readers += st.readers
		}
	}
	return readers, ""
}

// literalQuery vets query('<sql>') / query_table('<name>') with one plain
// literal argument by checking the SQL it carries as a read of its own.
func (a *analyzer) literalQuery(name string, toks []lexTok, i, depth int) (int, string) {
	if i+3 >= len(toks) || toks[i+2].Kind != lkString || !toks[i+2].Plain || !toks[i+3].isParen(")") {
		return 0, fmt.Sprintf("%s() is not allowed unless its argument is one plain string literal", name)
	}
	inner := toks[i+2].Text
	if name == "query_table" {
		parts, next, ok := parseNameFromString(inner)
		if !ok || next != len(inner) {
			return 0, "query_table() needs one plain table name"
		}
		st := &walkState{cte: map[string]bool{}}
		a.checkRef(parts, st)
		return st.readers, st.reason
	}
	if depth >= 2 {
		return 0, "query() nests too deeply to verify"
	}
	sub := *a
	sub.src = inner
	stmts := lexStatements(inner)
	if len(stmts) != 1 {
		return 0, "query() needs exactly one statement"
	}
	if n, err := countStatements(inner); err != nil || n != 1 {
		return 0, "query() needs exactly one statement"
	}
	v := sub.statement(stmts[0], nil, depth+1)
	if v.Class != sqlClassRead {
		if v.Reason != "" {
			return 0, v.Reason
		}
		return 0, "query() may only run a read statement"
	}
	a.vetted[inner] = true
	return v.readers, ""
}

func parseNameFromString(s string) ([]string, int, bool) {
	toks := lexStatements(s)
	if len(toks) != 1 {
		return nil, 0, false
	}
	parts, next, ok := parseName(toks[0], 0)
	if !ok || next != len(toks[0]) {
		return nil, 0, false
	}
	return parts, len(s), true
}

// vetReaderTokens checks a reader call's arguments: exactly one positional
// argument, a plain string literal, that the path policy accepts. Named
// arguments (header = true, columns = {...}) are not paths and pass.
func (a *analyzer) vetReaderTokens(fn string, toks []lexTok, open int) string {
	closeIdx := matchParen(toks, open)
	const need = "%s() needs its path written out as one plain string literal (no expressions, variables, lists, concatenation or E'...' strings)"
	if closeIdx < 0 || open+2 > closeIdx {
		return fmt.Sprintf(need, fn)
	}
	first := toks[open+1]
	if first.Kind != lkString || !first.Plain {
		return fmt.Sprintf(need, fn)
	}
	if after := toks[open+2]; !(after.isPunct(",") || after.isParen(")")) {
		return fmt.Sprintf(need, fn) // 'a' 'b' adjacent literals, 'a' || 'b', 'a'::T ...
	}
	// A second positional string (after a comma, not after "=" or ":=") would
	// be a second path.
	depth := 0
	for j := open + 2; j < closeIdx; j++ {
		switch {
		case toks[j].isParen("("), toks[j].isPunct("{"), toks[j].isPunct("["):
			depth++
		case toks[j].isParen(")"), toks[j].isPunct("}"), toks[j].isPunct("]"):
			depth--
		case depth == 0 && toks[j].isPunct(",") && j+1 < closeIdx:
			if n := toks[j+1]; n.Kind == lkString {
				return fmt.Sprintf("%s() takes one path: more than one positional argument is not allowed", fn)
			}
		}
	}
	// Named options can name other files or change how the reader resolves
	// them (version_name_format, allow_moved_paths, rejects_table ...): only an
	// explicit list of options that cannot is accepted.
	depth = 0
	for j := open + 2; j < closeIdx; j++ {
		switch {
		case toks[j].isParen("("), toks[j].isPunct("{"), toks[j].isPunct("["):
			depth++
		case toks[j].isParen(")"), toks[j].isPunct("}"), toks[j].isPunct("]"):
			depth--
		case depth == 0 && toks[j].isName() && j+1 < closeIdx:
			eq := toks[j+1].isPunct("=") || (toks[j+1].isPunct(":") && j+2 < closeIdx && toks[j+2].isPunct("="))
			if eq && !safeReaderParams[strings.ToLower(nameOf(toks[j]))] {
				return fmt.Sprintf("%s(): the option %q is not on the agent's list of safe reader options (%s)", fn, strings.ToLower(nameOf(toks[j])), safeReaderParamList())
			}
		}
	}
	r := a.paths.vetRead(first.Text)
	if r == "" {
		a.vetted[first.Text] = true
	}
	return r
}

// isTableRefStart reports whether toks[i] begins a table reference: the token
// after FROM, JOIN or USING, in a comma-separated FROM list, at the start of a
// parenthesised table group, or a string after TABLE.
func isTableRefStart(toks []lexTok, i int) bool {
	t := toks[i]
	if !(t.isName() || t.Kind == lkString) || i == 0 {
		return false
	}
	// A keyword after "(" starts a subquery, not a table name.
	if t.isWord("LATERAL") || t.isWord("SELECT") || t.isWord("WITH") || t.isWord("VALUES") || t.isWord("UNNEST") ||
		t.isWord("FROM") || t.isWord("TABLE") || t.isWord("PIVOT") || t.isWord("UNPIVOT") || t.isWord("DESCRIBE") || t.isWord("SHOW") || t.isWord("SUMMARIZE") {
		return false
	}
	prev := toks[i-1]
	switch {
	case prev.isWord("FROM"), prev.isWord("JOIN"), prev.isWord("USING"):
		return true
	case prev.isWord("PIVOT"), prev.isWord("UNPIVOT"):
		// UNPIVOT '<file>' ...; but FROM t UNPIVOT INCLUDE NULLS (...) is not a source.
		return !(t.isWord("INCLUDE") || t.isWord("EXCLUDE"))
	case prev.isWord("TABLE"):
		return t.Kind == lkString
	case prev.isParen("("):
		return i >= 2 && fromGroup(toks, i-2, 0)
	case prev.isPunct(","):
		return inFromList(toks, i-1)
	}
	return false
}

// fromGroup reports whether the "(" at toks[q+1] opens a group of table refs.
func fromGroup(toks []lexTok, q, depth int) bool {
	if q < 0 || depth > 4 {
		return false
	}
	switch {
	case toks[q].isWord("FROM"), toks[q].isWord("JOIN"):
		return true
	case toks[q].isPunct(","):
		return inFromList(toks, q)
	case toks[q].isParen("("):
		return fromGroup(toks, q-1, depth+1)
	}
	return false
}

// inFromList reports whether the comma at toks[c] separates table references:
// walking back at the same depth, FROM or USING comes before any other clause.
func inFromList(toks []lexTok, c int) bool {
	depth := 0
	for j := c - 1; j >= 0; j-- {
		t := toks[j]
		switch {
		case t.isParen(")"):
			depth++
		case t.isParen("("):
			if depth == 0 {
				return false
			}
			depth--
		case depth > 0:
		case t.Kind == lkWord:
			switch t.Text {
			case "FROM", "USING":
				return true
			case "SELECT", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "SET", "VALUES", "QUALIFY", "WINDOW", "RETURNING", "INTO", "BY":
				return false
			}
		}
	}
	return false
}

// tableRefAt parses the table name at toks[i]: one quoted or bare name, a
// dotted chain, or a single string literal (DuckDB reads 'x.csv' like "x.csv").
func tableRefAt(toks []lexTok, i int) (parts []string, next int, ok bool) {
	if toks[i].Kind == lkString {
		return []string{toks[i].Text}, i + 1, true
	}
	return parseName(toks, i)
}

// defScreen applies the token screen to a stored view or macro definition.
func (a *analyzer) defScreen(def string, depth int) (int, string) {
	if depth > 4 {
		return 0, "views and macros nest too deeply to verify"
	}
	a.catalog()
	a.functions()
	a.databases()
	sub := *a
	sub.src = def
	total := 0
	for _, toks := range lexStatements(def) {
		n, r := sub.screenTokens(toks, false, depth)
		if r != "" {
			return 0, r
		}
		total += n
	}
	return total, ""
}

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
	if st.summary > 0 && a.isRemote(catalogName, schema) {
		st.fail("SUMMARIZE would copy the whole table out of a remote connection; use SELECT with aggregates or a LIMIT instead")
		return
	}
	if isDuckLakeInternal(catalogName) || isDuckLakeInternal(schema) {
		st.fail("%s", duckLakeInternalMsg)
		return
	}
	if a.mysqlServerSchema(catalogName, schema) {
		st.fail("%s", mysqlSystemSchemaMsg)
		return
	}
	full := dotJoin(catalogName, schema, table)
	fileish := looksLikeFile(full)
	dbs := a.databases()
	userCat := func(n string) (dbRow, bool) {
		r, ok := dbs[strings.ToLower(n)]
		return r, ok && !(r.internal && strings.EqualFold(r.name, "system"))
	}
	lc := strings.ToLower(catalogName)
	switch {
	case lc == "system":
		if allowedSystemObjects[systemObjectKey(schema, table)] {
			return
		}
		st.fail("%s", unresolvedTableDeniedMsg(catalogName, schema, table))
		return
	case catalogName != "":
		if row, ok := userCat(catalogName); ok && !fileish && serverSide(row) {
			return // a server's own objects (views run on that server); DuckDB reports a missing table
		}
	case schema != "":
		if row, ok := userCat(schema); ok && !fileish && serverSide(row) {
			return
		}
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
		if _, ok := userCat(catalogName); ok {
			return
		}
	}
	if catalogName == "" && schema != "" {
		if _, ok := userCat(schema); ok {
			return
		}
	}
	st.fail("%s", unresolvedTableDeniedMsg(catalogName, schema, table))
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
