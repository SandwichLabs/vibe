package agents

import "fmt"

// SandboxMode defines the permission level for agent operations.
type SandboxMode string

const (
	ModeReadOnly  SandboxMode = "read-only"
	ModeReadWrite SandboxMode = "read-write"
	ModeFull      SandboxMode = "full"
)

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
