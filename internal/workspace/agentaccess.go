package workspace

import (
	"database/sql"

	"github.com/sandwich-labs/vibe-app/internal/llm/skills"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// AgentAccess is a workspace as the agent sees it: its DuckDB hub plus its
// file-backed notebook, query and skill stores. It satisfies
// agents.ContainerAccess (internal/agents defines the interface; this package
// need not import it), so agents.NewHandler(workspace.NewAgentAccess(ws, sess),
// engine.NewManager(sess), …) is the workspace-backed agent surface. The
// policy comes from Workspace.AgentPolicy, which satisfies agents.PolicySource
// as is.
//
// It owns nothing: closing the workspace and the session stays with the caller.
type AgentAccess struct {
	ws   Workspace
	sess session.Session
}

// NewAgentAccess pairs a workspace with its open session.
func NewAgentAccess(ws Workspace, sess session.Session) *AgentAccess {
	return &AgentAccess{ws: ws, sess: sess}
}

// DuckDBDB is the hub's database handle (nil when there is no session).
func (a *AgentAccess) DuckDBDB() *sql.DB {
	if a.sess == nil {
		return nil
	}
	return a.sess.DB()
}

// Session is the catalog-aware hub.
func (a *AgentAccess) Session() session.Session { return a.sess }

// Notebooks is the workspace's notebooks/ folder.
func (a *AgentAccess) Notebooks() notebooks.Store { return a.ws.Notebooks() }

// Queries is the workspace's queries/ folder.
func (a *AgentAccess) Queries() queries.Store { return a.ws.Queries() }

// Skills is the workspace's skills/ folder.
func (a *AgentAccess) Skills() skills.Store { return a.ws.Skills() }

// AgentPolicy is the workspace's effective policy for this person.
func (a *AgentAccess) AgentPolicy() (string, bool) { return a.ws.AgentPolicy() }
