package agents

// DefaultSandboxConfig returns a sandbox config with read-only defaults.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		Mode:         ModeReadOnly,
		MaxQueryRows: 1000,
	}
}
