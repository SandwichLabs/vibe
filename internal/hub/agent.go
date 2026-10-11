package hub

import (
	"fmt"
	"log/slog"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// ResolveSandbox is the workspace's effective sandbox tightened, never
// loosened, by an extra request (the --sandbox flag). An empty request keeps
// base; an unrecognised one is an error.
func ResolveSandbox(base agents.SandboxMode, request string) (agents.SandboxMode, error) {
	if request == "" {
		return base, nil
	}
	mode, err := agents.ParseSandboxMode(request)
	if err != nil {
		return "", fmt.Errorf("invalid --sandbox %q: want read-only, read-write, or full", request)
	}
	return agents.Tighter(base, mode), nil
}

// NewAgentHandler builds the agent tool handler for an open workspace: its
// file-backed stores and hub (workspace.AgentAccess) under the workspace's
// effective policy (Workspace.AgentPolicy: the committed ceiling and this
// person's setting), further tightened by tighten ("" = no change).
//
// No approver is attached. A write that needs approval is refused until the
// owner (Studio) calls Handler.SetApprover; a headless process has nobody to
// ask. The returned policy is what the handler was built with.
func NewAgentHandler(ws workspace.Workspace, sess session.Session, tighten string, logger *slog.Logger) (*agents.Handler, agents.FilePolicy, error) {
	if logger == nil {
		logger = slog.Default()
	}
	base := agents.PolicyFrom(ws)
	mode, err := ResolveSandbox(base.Mode, tighten)
	if err != nil {
		return nil, agents.FilePolicy{}, err
	}
	policy := agents.FilePolicy{Mode: mode, AskBeforeWrites: base.AskBeforeWrites}
	cfg := &agents.SandboxConfig{Mode: policy.Mode, MaxQueryRows: 10000, RequireApproval: policy.AskBeforeWrites}
	eng := engine.NewManager(sess, engine.WithLogger(logger))
	return agents.NewHandler(workspace.NewAgentAccess(ws, sess), eng, cfg, logger), policy, nil
}
