package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/engine"
)

// This file is the save-time vet of a data artifact (D5, epic #162). The
// structure of a manifest is internal/artifacts' job; whether its queries may
// run is the agent SQL policy's, so the vet lives here. The run-time gate
// (RunArtifactQuery) repeats every check below, because a file can arrive by
// git, an editor or a bundle without ever passing through the agent.

// ArtifactVetError is a manifest whose queries did not pass the agent's read
// policy. Problems has one entry per refused query, each naming the query and
// giving the policy's reason, so a caller can show them as a list.
type ArtifactVetError struct {
	Problems []string
}

func (e *ArtifactVetError) Error() string {
	return "the artifact's queries did not pass the agent's read policy:\n" + strings.Join(e.Problems, "\n")
}

// VetArtifact runs every query of m (structurally valid already) through the
// same read-only policy a run goes through, with each parameter at its default:
// the connection is validated, the lexical checks run, then wildcard expansion,
// classification and the read-only gate (readOnlyVerdict). A refusal is an
// *ArtifactVetError naming each failing query and the policy's reason.
func (h *Handler) VetArtifact(ctx context.Context, m *artifacts.Manifest) error {
	var problems []string
	for _, q := range m.Queries {
		if err := h.vetArtifactQuery(ctx, m, q.Name); err != nil {
			problems = append(problems, fmt.Sprintf("query %q: %s", q.Name, redactForAgent(err.Error())))
		}
	}
	if len(problems) > 0 {
		return &ArtifactVetError{Problems: problems}
	}
	return nil
}

// vetArtifactQuery vets one query with its defaults bound, on the connection
// it will run on.
func (h *Handler) vetArtifactQuery(ctx context.Context, m *artifacts.Manifest, name string) error {
	sqlText, connection, params, err := artifacts.Bind(m, name, nil, time.Now())
	if err != nil {
		return err
	}
	runConn, err := h.checkArtifactStatement(sqlText, connection)
	if err != nil {
		return err
	}
	_, _, reason, err := h.readOnlyVerdict(ctx, runConn, sqlText, params)
	if err != nil {
		if errors.Is(err, ErrReadOnlyBindingDenied) && reason != "" {
			return errors.New(reason)
		}
		return err
	}
	return nil
}

// checkArtifactStatement is the part of the vet that needs no database and
// that the run repeats first: the lexical checks on the SQL, and the query's
// connection resolved to its canonical registered name ("" is the session
// default). Vet and run both resolve the connection here, so a statement is
// never vetted on one connection and run on another.
func (h *Handler) checkArtifactStatement(sqlText, connection string) (string, error) {
	if err := checkArtifactSQLShape(sqlText); err != nil {
		return "", err
	}
	return h.validConnection(connection)
}

// readerOrQueryArgs are the functions whose argument decides what is read or
// run, so a parameter may never appear inside them.
var readerOrQueryArgs = func() map[string]bool {
	m := map[string]bool{"query": true, "query_table": true}
	for n := range readerFuncs {
		m[strings.ToLower(n)] = true
	}
	return m
}()

// checkArtifactSQLShape holds a query to the two lexical rules of D5, on the
// policy's own lexer (sql_lex.go):
//
//   - a query is exactly one statement (the lexer's count; the policy
//     cross-checks it against DuckDB's before any classification);
//   - every $name placeholder is a parameter token. engine.BindParams rewrites
//     every "$name" it finds in the raw text, string literals, quoted names,
//     $$...$$ bodies and comments included, and the database would quietly
//     ignore the extra argument, so a placeholder in any of those places is
//     refused, as are a bare "?" and a positional "$1" (they would take another
//     parameter's argument);
//   - a parameter is never the argument of query(), query_table() or a reader.
//
// It is cheap and does no I/O.
func checkArtifactSQLShape(sqlText string) error {
	var seen []string
	stmts := lexStatements(sqlText)
	if len(stmts) != 1 {
		return errors.New("a query is exactly one statement")
	}
	for _, toks := range stmts {
		for i, t := range toks {
			if t.Kind != lkPunct {
				continue
			}
			switch t.Text {
			case "?":
				return errors.New("a bare ? is not allowed: write the parameter as $name")
			case "$":
				if i+1 >= len(toks) || toks[i+1].Kind != lkWord || toks[i+1].Start != t.End {
					return errors.New("a $ must start a parameter name such as $from")
				}
				name := toks[i+1].Raw
				if name[0] >= '0' && name[0] <= '9' {
					return fmt.Errorf("$%s is a positional parameter: use a declared name such as $from", name)
				}
				seen = append(seen, name)
			}
		}
		if err := checkParamInReaderArgs(toks); err != nil {
			return err
		}
	}
	if !slices.Equal(seen, engine.ParamOccurrences(sqlText)) {
		return errors.New("a $name placeholder appears inside a string literal, a quoted name, a $$...$$ body or a comment, where it would be rewritten too: " +
			"keep $name only where a value belongs, and write literal text without it")
	}
	return nil
}

// checkParamInReaderArgs refuses a parameter inside the argument list of
// query(...), query_table(...) or a file reader in one statement's tokens.
func checkParamInReaderArgs(toks []lexTok) error {
	for i := 0; i+1 < len(toks); i++ {
		if !toks[i].isName() || !toks[i+1].isParen("(") || !readerOrQueryArgs[strings.ToLower(toks[i].Text)] {
			continue
		}
		end := matchParen(toks, i+1)
		if end < 0 {
			end = len(toks)
		}
		for _, t := range toks[i+2 : end] {
			if t.Kind == lkPunct && t.Text == "$" {
				return fmt.Errorf("a parameter cannot be an argument of %s(...): it would choose what is read or run", strings.ToLower(toks[i].Text))
			}
		}
	}
	return nil
}

// artifactQueryRowCap bounds the rows one artifact query returns, the cap of a
// live A2UI binding. The cap is pushed into the query (RunReadOnlySQLIn).
const artifactQueryRowCap = 500

// RunArtifactQuery runs one named query of a stored artifact with the given
// parameter values, and is the ONE path that does: Studio's binding, the
// level-2 bridge and any later tool call it. It never trusts the caller for
// anything but names and values:
//
//   - the artifact is loaded from the store (not from the caller), so what
//     runs is what is on disk;
//   - the lexical checks of the save-time vet repeat here, and the query's
//     connection is re-validated, because a file edited by hand or imported
//     in a bundle never passed the vet;
//   - artifacts.Bind turns the values into prepared-statement arguments (a
//     value is never SQL text) and refuses an unknown name or a bad value;
//   - RunReadOnlySQLIn then expands, classifies with the ACTUAL values and
//     applies the read-only gate, so the plan check sees what will run, not
//     the defaults. A denial there is the fixed ErrReadOnlyBindingDenied.
//
// The result belongs to the caller's UI. No agent tool returns it.
func (h *Handler) RunArtifactQuery(ctx context.Context, name, query string, values map[string]any) (*engine.ResultSet, bool, error) {
	store, err := h.artifactStore()
	if err != nil {
		return nil, false, err
	}
	a, err := store.Get(ctx, name)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load artifact %q: %w", name, err)
	}
	sqlText, connection, params, err := artifacts.Bind(&a.Manifest, query, values, time.Now())
	if err != nil {
		return nil, false, err
	}
	runConn, err := h.checkArtifactStatement(sqlText, connection)
	if err != nil {
		h.logger.Debug("artifact query refused", "artifact", name, "query", query, "reason", err)
		return nil, false, err
	}
	return h.RunReadOnlySQLIn(ctx, runConn, sqlText, params, artifactQueryRowCap)
}
