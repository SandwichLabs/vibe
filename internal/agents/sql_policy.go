package agents

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
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
		return deny(fmt.Sprintf("%s: %s", couldntVerifyMsg, parseFailure(err)))
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

// parseFailure is the reason for a statement DuckDB's parser rejected. A name
// with four or more parts (motherduck.newsroom.raw.t) fails deep in the
// serializer with "NameListToString NOT IMPLEMENTED", which says nothing to
// the agent, so it is explained (#111).
func parseFailure(err error) string {
	msg := firstLine(err.Error())
	if strings.Contains(msg, "NameListToString") {
		return "a table name has at most three parts (connection.schema.table); " + oneDatabasePerConnectionHint
	}
	return msg
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
		return deny(fmt.Sprintf("%s: %s", couldntVerifyMsg, parseFailure(err)))
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
