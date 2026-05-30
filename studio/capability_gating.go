package main

// Phase 4 / unit P4.4: "Capability gating: deny privileged app.go methods +
// force agent sandbox in App Mode" (parity plan §4.3, risk §8).
//
// STUB ONLY (TDD RED). These symbols exist so the capability-gating integration
// test compiles. They intentionally do NOT yet enforce lockdown — guardStudioOnly
// is not wired into the privileged bindings and agentSandboxForMode does not yet
// cap App Mode at read-write. The RED test asserts the intended behavior and must
// FAIL until P4.4 is implemented.

import (
	"errors"
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/agents"
)

// errCapabilityDenied is returned by privileged Studio-only bindings when they
// are invoked while the live mode is "app".
var errCapabilityDenied = errors.New("capability denied: operation not available in App Mode")

// guardStudioOnly returns errCapabilityDenied when the live runtime mode is
// "app", and nil otherwise. Privileged bindings (schema/SQL/page/definition
// edits, imports) call this at the top to enforce App-Mode lockdown.
func (a *App) guardStudioOnly(method string) error {
	if a.mode == modeApp {
		a.logger.Warn("capability denied in App Mode", "method", method)
		return fmt.Errorf("%w: %s", errCapabilityDenied, method)
	}
	return nil
}

// agentSandboxForMode returns the agent SandboxConfig to use for the given live
// app mode. In App Mode it caps the sandbox at read-write (never full) so the
// agent cannot run schema/page tool categories; in Studio Mode it returns full
// access.
func agentSandboxForMode(mode string) *agents.SandboxConfig {
	if mode == modeApp {
		return &agents.SandboxConfig{Mode: agents.ModeReadWrite, MaxQueryRows: 10000}
	}
	return &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 10000}
}
