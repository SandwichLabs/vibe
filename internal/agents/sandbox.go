package agents

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/sandbox"
)

// SandboxMode defines the permission level for agent operations. The tiers,
// their parser and their order live in internal/sandbox, shared with the
// workspace policy.
type SandboxMode = sandbox.Mode

const (
	ModeReadOnly  = sandbox.ReadOnly
	ModeReadWrite = sandbox.ReadWrite
	ModeFull      = sandbox.Full
)

// TaskSandbox is the tier task mode ALWAYS runs at, whatever the workspace's
// own agent setting: task mode proposes and never mutates (write_cell only
// records a proposal the person must still Apply), so it never needs more than
// read access.
const TaskSandbox = ModeReadOnly

// DefaultSandboxMode is the sandbox mode of a source that states none (no
// PolicySource, or an empty one).
const DefaultSandboxMode = ModeReadWrite

// PolicySource supplies the agent's sandbox mode and ask-before-writes gate.
// It is the one seam between the agent and wherever the policy lives (#109):
// a workspace feeds it from its effective policy (committed ceiling tightened
// by this person's local setting; internal/workspace.Workspace.AgentPolicy
// implements this interface as is). Implementations return plain values so the source
// never imports this package.
type PolicySource interface {
	// AgentPolicy returns the sandbox mode string (read-only, read-write or
	// full) and whether every agent write needs the user's OK.
	AgentPolicy() (sandbox string, askBeforeWrites bool)
}

// PolicyFrom resolves a FilePolicy from any source, failing closed exactly
// closed: an unrecognised sandbox string becomes read-only, never
// the default, and a nil source yields the defaults.
func PolicyFrom(src PolicySource) FilePolicy {
	policy := FilePolicy{Mode: DefaultSandboxMode, AskBeforeWrites: true}
	if src == nil {
		return policy
	}
	sandbox, ask := src.AgentPolicy()
	if mode, err := ParseSandboxMode(sandbox); err == nil {
		policy.Mode = mode
	} else {
		policy.Mode = ModeReadOnly
	}
	policy.AskBeforeWrites = ask
	return policy
}

// FilePolicy is the resolved, ready-to-use agent policy for one open file:
// which sandbox mode it runs at, and whether a write needs the user's OK
// first.
type FilePolicy struct {
	Mode            SandboxMode
	AskBeforeWrites bool
}

// ParseSandboxMode strictly parses s as one of the three known sandbox mode
// strings, for callers that must reject anything else outright (SetAgentConfig,
// the --sandbox flag) rather than fail closed the way PolicyFrom does for
// a possibly-stale value.
func ParseSandboxMode(s string) (SandboxMode, error) { return sandbox.Parse(s) }

// Tighter returns whichever of a/b is the MORE restrictive sandbox mode
// (read-only < read-write < full). Used to combine a caller-fixed ceiling
// (e.g. AgentTask's "always read-only, regardless of the file's own
// setting" contract) with the file's live "agent_sandbox" setting: the
// effective mode a dispatch runs at is never more permissive than either
// one. An unrecognized value is returned as is (see sandbox.Tighter).
func Tighter(a, b SandboxMode) SandboxMode { return sandbox.Tighter(a, b) }

// SandboxConfig defines permission boundaries for an agent session.
type SandboxConfig struct {
	Mode              SandboxMode
	AllowedTables     []string
	MaxQueryRows      int
	RequireApproval   bool
	BlockedOperations []string
}

// categoryPermissions maps sandbox modes to the set of allowed tool categories.
var categoryPermissions = map[SandboxMode]map[string]bool{
	ModeReadOnly: {
		"introspect": true,
	},
	// Content (notebook cells, skills) is workspace files, not data: a
	// read-write agent can write up what it finds (#111). Schema stays full.
	ModeReadWrite: {
		"introspect": true,
		"data":       true,
		"content":    true,
	},
	ModeFull: {
		"introspect": true,
		"data":       true,
		"content":    true,
		"schema":     true,
	},
}

// Check returns an error if the given method is not allowed under the current sandbox mode.
func (sc *SandboxConfig) Check(method string) error {
	// Check blocked operations first.
	for _, blocked := range sc.BlockedOperations {
		if blocked == method {
			return fmt.Errorf("operation %q is blocked by sandbox configuration", method)
		}
	}

	// run_query is exempt from the blanket "data" category gate: it carries
	// arbitrary SQL, so its permission depends on what that SQL actually does
	// (a SELECT is safe in read-only; a CREATE TABLE is not safe just because
	// read-write allows "data"). handleRunQuery classifies the statement and
	// calls checkSQLClass itself instead. (Handler.Handle already rejects an
	// unknown method before calling Check, so no separate existence check is
	// needed here.)
	if method == "run_query" {
		return nil
	}

	tool := GetTool(method)
	if tool == nil {
		return fmt.Errorf("unknown method: %q", method)
	}

	allowed, ok := categoryPermissions[sc.Mode]
	if !ok {
		return fmt.Errorf("unknown sandbox mode: %q", sc.Mode)
	}

	if !allowed[tool.Category] {
		return fmt.Errorf("method %q (category %q) is not allowed in %s mode", method, tool.Category, sc.Mode)
	}

	return nil
}

// IsTableAllowed checks if a table can be accessed. If AllowedTables is empty, all tables are allowed.
func (sc *SandboxConfig) IsTableAllowed(table string) bool {
	if len(sc.AllowedTables) == 0 {
		return true
	}
	for _, t := range sc.AllowedTables {
		if t == table {
			return true
		}
	}
	return false
}

type sandboxCeilingKey struct{}

// WithSandboxCeiling returns a context that caps every call dispatched with it
// at mode (tighten-only): the effective mode is the stricter of the handler's
// own and mode. MCP uses it to bind a connection's requested sandbox (a proxy's
// --sandbox) to its session. An unknown mode caps at read-only.
func WithSandboxCeiling(ctx context.Context, mode SandboxMode) context.Context {
	if _, err := ParseSandboxMode(string(mode)); err != nil {
		mode = ModeReadOnly
	}
	return context.WithValue(ctx, sandboxCeilingKey{}, mode)
}

// sandboxFor loads the handler's sandbox once for a call, tightened by any
// ceiling on ctx. The handler's own config is never modified.
func (h *Handler) sandboxFor(ctx context.Context) *SandboxConfig {
	cur := h.sandbox.Load()
	ceil, ok := ctx.Value(sandboxCeilingKey{}).(SandboxMode)
	if !ok || cur == nil {
		return cur
	}
	tight := Tighter(cur.Mode, ceil)
	if tight == cur.Mode {
		return cur
	}
	next := *cur
	next.Mode = tight
	return &next
}
