package workspace

import (
	"context"
	"fmt"
	"time"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/usersql"
)

// PersonExpansion is a statement a person typed, with its metric() calls
// expanded.
type PersonExpansion struct {
	// SQL is the text to check and run.
	SQL string
	// Params are the caller's values plus the declared defaults of the
	// metrics' saved-query parameters.
	Params map[string]any
	// Uses are the metrics the statement expanded, in order.
	Uses []semantics.Use
}

// BodyVetter holds the SQL a metric hides to the agent's read-only policy
// (hub.MetricVetter, built on agents.Handler.VetReadOnlySQLIn). The workspace
// package cannot import agents, so each surface injects it.
type BodyVetter func(ctx context.Context, connection, sql string, params map[string]any) error

// PersonOptions are what a surface adds to an expansion.
type PersonOptions struct {
	// Vet is applied to every expanded metric (its saved query and measure)
	// after the structural checks. The person's own SQL around the metric is
	// not run through it. nil skips the policy (tests of the expander only).
	Vet BodyVetter
	// Declared are the parameters an open saved query declares, so a metric
	// parameter of another type under the same name is reported as a clash.
	Declared []queries.Param
}

// ExpandForPerson expands metric('name', by => ...) calls in text a person
// typed (the Query Explorer, `vibe query`, a notebook's console, `vibe repl`)
// before the editor's gate looks at it, with the same expander the agent's
// policy uses (internal/semantics). connection is the connection the statement
// runs on ("" is the workspace default).
//
// The person's editor has always run SQL as written, but a metric hides a
// saved query behind a name, so each expanded saved query must also be exactly
// one SELECT by DuckDB's own parser (usersql.RequireSelect): a hand-edited or
// git-pulled saved query behind a metric cannot do more than read. Text with no
// metric call comes back unchanged with params as given.
func ExpandForPerson(ctx context.Context, ws Workspace, sess session.Session, text, connection string, params map[string]any, opts PersonOptions) (*PersonExpansion, error) {
	out := &PersonExpansion{SQL: text, Params: params}
	if !semantics.HasMetricCall(text) || ws == nil || sess == nil {
		return out, nil
	}
	res, err := semantics.Expand(ctx, text, semantics.NewReader(ws.Semantics(), ws.Queries()),
		semantics.Options{Connection: connection, DefaultConnection: sess.DefaultCatalog()})
	if err != nil {
		return nil, err
	}
	if !res.Changed {
		return out, nil
	}
	for _, b := range res.Bodies {
		if err := usersql.RequireSelect(ctx, sess.DB(), b.SQL); err != nil {
			return nil, fmt.Errorf("metric %q: its saved query %q is not a plain read: %w", b.Metric, b.Query, err)
		}
	}
	if err := res.CheckOuterParams(text, params, opts.Declared); err != nil {
		return nil, err
	}
	params, err = res.BindParams(params, time.Now())
	if err != nil {
		return nil, err
	}
	if err := vetUses(ctx, res, connection, params, opts.Vet); err != nil {
		return nil, err
	}
	return &PersonExpansion{SQL: res.SQL, Params: params, Uses: res.Uses}, nil
}

// vetUses runs each expanded metric (as a SELECT over its piece, with only the
// parameters it uses) through vet, naming the metric in a refusal.
func vetUses(ctx context.Context, res *semantics.Result, connection string, params map[string]any, vet BodyVetter) error {
	if vet == nil {
		return nil
	}
	for _, u := range res.Uses {
		stmt := "SELECT * FROM " + u.Piece
		own := map[string]any{}
		for _, n := range engine.ParamNames(stmt) {
			if v, ok := params[n]; ok {
				own[n] = v
			}
		}
		if err := vet(ctx, connection, stmt, own); err != nil {
			return fmt.Errorf("metric '%s' is refused: %w", u.Name, err)
		}
	}
	return nil
}
