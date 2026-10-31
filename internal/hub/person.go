package hub

import (
	"context"
	"log/slog"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/usersql"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// PersonGuard is the courtesy gate a surface holds SQL a person typed to
// (usersql.CheckInteractive for Studio and the console, usersql.Check for the
// CLI and the TUI). nil skips it.
type PersonGuard func(ctx context.Context, sql string) error

// InteractiveGuard is the Studio query editor's and the console's gate.
func InteractiveGuard(sess session.Session) PersonGuard {
	return func(ctx context.Context, text string) error {
		return usersql.CheckInteractive(ctx, sess, text)
	}
}

// PersonRun is a statement a person typed, to be prepared.
type PersonRun struct {
	// SQL is the text as typed (or the saved query's text).
	SQL string
	// Connection is the connection the statement runs on ("" is the default).
	Connection string
	// Params are the values already bound to a saved query's parameters.
	Params map[string]any
	// Declared are the parameters an open saved query declares.
	Declared []queries.Param
	// Guard holds the typed text, and the expanded text when a metric() call
	// changed it, to the surface's courtesy checks. nil skips both.
	Guard PersonGuard
}

// PrepareForPerson is the one step every person-facing run path takes before
// SQL reaches the database: the guard on the text as typed, metric() calls
// expanded (workspace.ExpandForPerson, each saved query behind a metric held to
// the agent's read-only policy), and, when anything expanded, the guard again
// on what will really run. It runs nothing. The result's SQL, Params and Uses
// are what to run, bind and report; callers that run the text inside COPY
// (exports) or hand it to an engine use these, never the typed text.
func PrepareForPerson(ctx context.Context, ws workspace.Workspace, sess session.Session, logger *slog.Logger, run PersonRun) (*workspace.PersonExpansion, error) {
	if run.Guard != nil {
		if err := run.Guard(ctx, run.SQL); err != nil {
			return nil, err
		}
	}
	opts := workspace.PersonOptions{Declared: run.Declared}
	if semantics.HasMetricCall(run.SQL) && ws != nil && sess != nil {
		vet, err := MetricVetter(ws, sess, logger)
		if err != nil {
			return nil, err
		}
		opts.Vet = vet
	}
	exp, err := workspace.ExpandForPerson(ctx, ws, sess, run.SQL, run.Connection, run.Params, opts)
	if err != nil {
		return nil, err
	}
	if len(exp.Uses) > 0 && run.Guard != nil {
		// What runs is the expanded text, so the guard reads that too.
		if err := run.Guard(ctx, exp.SQL); err != nil {
			return nil, err
		}
	}
	return exp, nil
}

// PersonExpander is repl.Options.Expand for a console: PrepareForPerson with no
// guard, because the console session holds the expanded text to
// usersql.CheckInteractive itself.
func PersonExpander(ws workspace.Workspace, sess session.Session, logger *slog.Logger) func(ctx context.Context, text, connection string) (string, map[string]any, error) {
	return func(ctx context.Context, text, connection string) (string, map[string]any, error) {
		exp, err := PrepareForPerson(ctx, ws, sess, logger, PersonRun{SQL: text, Connection: connection})
		if err != nil {
			return "", nil, err
		}
		return exp.SQL, exp.Params, nil
	}
}

// PrepareExport is PrepareForPerson for text that goes inside COPY (...): the
// expanded statement must also be exactly one SELECT by DuckDB's own parser, so
// nothing can be smuggled in after a closing parenthesis. Both exports (Studio's
// dialog and `vibe export --sql`) call it.
func PrepareExport(ctx context.Context, ws workspace.Workspace, sess session.Session, logger *slog.Logger, run PersonRun) (*workspace.PersonExpansion, error) {
	exp, err := PrepareForPerson(ctx, ws, sess, logger, run)
	if err != nil {
		return nil, err
	}
	if err := usersql.RequireSelect(ctx, sess.DB(), exp.SQL); err != nil {
		return nil, err
	}
	return exp, nil
}
