package agents

import (
	"context"
	"fmt"
)

// SandboxMode defines the permission level for agent operations.
type SandboxMode string

const (
	ModeReadOnly  SandboxMode = "read-only"
	ModeReadWrite SandboxMode = "read-write"
	ModeFull      SandboxMode = "full"
)

// Per-file agent policy (#79). The policy's one home is this file — the
// container package stays policy-free (container -> agents would be a
// layering inversion), and every caller (cmd/mcp.go, studio/app.go) reads it
// through ReadFilePolicy instead of re-deriving its own defaults.
const (
	// MetaKeySandbox is the _vibe_meta key holding the file's sandbox mode.
	// Existing files (created before #79) already have this key stored, most
	// commonly "full" — see ReadFilePolicy's doc comment for why a NEW file
	// no longer seeds it.
	MetaKeySandbox = "agent_sandbox"
	// MetaKeyAskBeforeWrites is the _vibe_meta key holding whether the agent
	// must get user approval before any write, on every surface (Studio chat
	// and MCP alike — one tool surface, one gate, no second per-surface
	// mode). Absent on every file that exists today, which is deliberate:
	// ReadFilePolicy treats an absent key as "on".
	MetaKeyAskBeforeWrites = "agent_ask_before_writes"
	// DefaultSandboxMode is what a file with no stored MetaKeySandbox runs
	// at. internal/container no longer seeds MetaKeySandbox on Create, so a
	// brand-new file's mode comes from here, not from a literal duplicated
	// in the schema.
	DefaultSandboxMode = ModeReadWrite
)

// MetaReader is the read side of container.Container's Meta method — kept as
// its own tiny interface here (rather than importing internal/container, a
// layering inversion) so ReadFilePolicy can be called with the real
// container, a test double, or nil.
type MetaReader interface {
	Meta(key string) (string, error)
}

// PolicySource supplies the agent's sandbox mode and ask-before-writes gate.
// It is the one seam between the agent and wherever the policy lives (#109):
// the legacy container feeds it from _vibe_meta (MetaPolicy), a workspace
// feeds it from its effective policy (committed ceiling tightened by this
// person's local setting; internal/workspace.Workspace.AgentPolicy implements
// this interface as is). Implementations return plain values so the source
// never imports this package.
type PolicySource interface {
	// AgentPolicy returns the sandbox mode string (read-only, read-write or
	// full) and whether every agent write needs the user's OK.
	AgentPolicy() (sandbox string, askBeforeWrites bool)
}

// MetaPolicy is the PolicySource over a legacy file's _vibe_meta keys. A nil
// reader yields the defaults.
func MetaPolicy(m MetaReader) PolicySource { return metaPolicy{m: m} }

type metaPolicy struct{ m MetaReader }

func (p metaPolicy) AgentPolicy() (string, bool) {
	fp := ReadFilePolicy(p.m)
	return string(fp.Mode), fp.AskBeforeWrites
}

// PolicyFrom resolves a FilePolicy from any source, failing closed exactly
// like ReadFilePolicy: an unrecognised sandbox string becomes read-only, never
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

// ReadFilePolicy resolves a file's agent policy from its stored _vibe_meta
// keys, failing closed on anything unexpected:
//   - MetaKeySandbox absent, or the reader errors reading it -> DefaultSandboxMode.
//   - MetaKeySandbox present but not one of the three known mode strings ->
//     ModeReadOnly (the most restrictive), NOT DefaultSandboxMode — an
//     unrecognized value is corruption or a future format, not "unset", and
//     the Settings UI should show the user the (safe) truth rather than
//     silently substituting a permissive default.
//   - MetaKeyAskBeforeWrites absent, errors, or anything other than the
//     exact string "false" -> true. This is the deliberate part of #79: a
//     panel-era file that already has agent_sandbox="full" stored keeps
//     that mode (no migration touches it — additive-migrations policy), but
//     since it has no agent_ask_before_writes key at all, it now asks
//     first. That's the only way to make those existing files safe without
//     silently downgrading their stored mode.
func ReadFilePolicy(m MetaReader) FilePolicy {
	policy := FilePolicy{Mode: DefaultSandboxMode, AskBeforeWrites: true}
	if m == nil {
		return policy
	}
	if raw, err := m.Meta(MetaKeySandbox); err == nil {
		if mode, perr := ParseSandboxMode(raw); perr == nil {
			policy.Mode = mode
		} else {
			policy.Mode = ModeReadOnly
		}
	}
	if raw, err := m.Meta(MetaKeyAskBeforeWrites); err == nil && raw == "false" {
		policy.AskBeforeWrites = false
	}
	return policy
}

// ParseSandboxMode strictly parses s as one of the three known sandbox mode
// strings, for callers that must reject anything else outright (SetAgentConfig,
// the --sandbox flag) rather than fail closed the way ReadFilePolicy does for
// an already-stored, possibly-stale value.
func ParseSandboxMode(s string) (SandboxMode, error) {
	switch SandboxMode(s) {
	case ModeReadOnly, ModeReadWrite, ModeFull:
		return SandboxMode(s), nil
	default:
		return "", fmt.Errorf("unknown sandbox mode: %q (want read-only, read-write, or full)", s)
	}
}

// modeStrictness ranks the three sandbox modes from most to least
// permissive-restricted (read-only is the MOST restrictive, full the LEAST),
// so Tighter can compare two modes without hand-rolling a switch at every
// call site. An unrecognized mode ranks as read-only (the safe default) —
// see Tighter's doc comment.
var modeStrictness = map[SandboxMode]int{
	ModeReadOnly:  0,
	ModeReadWrite: 1,
	ModeFull:      2,
}

// Tighter returns whichever of a/b is the MORE restrictive sandbox mode
// (read-only < read-write < full). Used to combine a caller-fixed ceiling
// (e.g. AgentTask's "always read-only, regardless of the file's own
// setting" contract) with the file's live "agent_sandbox" setting: the
// effective mode a dispatch runs at is never more permissive than either
// one. An unrecognized SandboxMode value (not one of the three constants)
// is returned immediately, without being ranked — SandboxConfig.Check
// already fails closed on an unknown mode ("unknown sandbox mode"), so
// propagating a bad/empty value as-is can only ever deny more, never grant
// access Tighter's caller didn't intend.
func Tighter(a, b SandboxMode) SandboxMode {
	ra, ok := modeStrictness[a]
	if !ok {
		return a
	}
	rb, ok := modeStrictness[b]
	if !ok {
		return b
	}
	if ra <= rb {
		return a
	}
	return b
}

// SandboxConfig defines permission boundaries for an agent session.
type SandboxConfig struct {
	Mode              SandboxMode
	AllowedTables     []string
	MaxQueryRows      int
	RequireApproval   bool
	BlockedOperations []string
}

// DefaultSandboxConfig returns a sandbox config with read-only defaults.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		Mode:         ModeReadOnly,
		MaxQueryRows: 1000,
	}
}

// categoryPermissions maps sandbox modes to the set of allowed tool categories.
var categoryPermissions = map[SandboxMode]map[string]bool{
	ModeReadOnly: {
		"introspect": true,
	},
	ModeReadWrite: {
		"introspect": true,
		"data":       true,
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
