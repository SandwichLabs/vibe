package semantics

import (
	"context"

	"github.com/SandwichLabs/vibe/internal/engine"
)

// QueryFunc runs one read statement on a connection ("" is the workspace
// default) with bound parameters. Assertions and join profiling take one
// instead of an engine, so they never choose how SQL is gated: the caller
// hands in the agent policy's read-only runner.
type QueryFunc func(ctx context.Context, connection, sql string, params map[string]any) (*engine.ResultSet, error)
