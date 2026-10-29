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

// MetricVetter is the read-only policy check a person's surface runs on the
// SQL a metric() call expands to (workspace.BodyVetter): the agent policy's
// own verdict, in the tightest tier, so a metric from a cloned repository is
// held to what a cloned artifact query is. It executes nothing.
func MetricVetter(ws workspace.Workspace, sess session.Session, logger *slog.Logger) (workspace.BodyVetter, error) {
	h, _, err := NewAgentHandler(ws, sess, string(agents.ModeReadOnly), logger)
	if err != nil {
		return nil, err
	}
	return h.VetReadOnlySQLIn, nil
}

// ReadOnlyRunner is the agent policy's read-only runner in the tightest tier,
// for the person's runs of what a definition declares (assertions, join
// profiling, a metric's "Try it"): every statement goes through the same
// gates as a cloned artifact query, whatever the sandbox setting.
func ReadOnlyRunner(ws workspace.Workspace, sess session.Session, logger *slog.Logger) (workspace.ReadOnlyRunner, error) {
	h, _, err := NewAgentHandler(ws, sess, string(agents.ModeReadOnly), logger)
	if err != nil {
		return nil, err
	}
	return h, nil
}
