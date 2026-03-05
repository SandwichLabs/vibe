package agents

import "fmt"

// SandboxMode defines the permission level for agent operations.
type SandboxMode string

const (
	ModeReadOnly  SandboxMode = "read-only"
	ModeReadWrite SandboxMode = "read-write"
	ModeFull      SandboxMode = "full"
)

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
		"page":       true,
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
