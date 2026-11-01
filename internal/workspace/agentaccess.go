package workspace

import (
	"database/sql"
	"path/filepath"

	"github.com/SandwichLabs/assay/internal/artifacts"

	"github.com/SandwichLabs/assay/internal/connections"

	"github.com/SandwichLabs/assay/internal/notebooks"
	"github.com/SandwichLabs/assay/internal/queries"
	"github.com/SandwichLabs/assay/internal/semantics"
	"github.com/SandwichLabs/assay/internal/semantics/proposals"
	"github.com/SandwichLabs/assay/internal/session"
	"github.com/SandwichLabs/assay/internal/skills"
)

// AgentAccess is a workspace as the agent sees it: its DuckDB hub plus its
// file-backed notebook, query and skill stores. It satisfies
// agents.WorkspaceAccess (internal/agents defines the interface; this package
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

// DataDir is the workspace's data/ folder: the only place the agent may read
// files from and COPY ... TO (agent SQL policy, #116).
func (a *AgentAccess) DataDir() string { return filepath.Join(a.ws.Root(), "data") }

// Session is the catalog-aware hub.
func (a *AgentAccess) Session() session.Session { return a.sess }

// Notebooks is the workspace's notebooks/ folder.
func (a *AgentAccess) Notebooks() notebooks.Store { return a.ws.Notebooks() }

// Queries is the workspace's queries/ folder.
func (a *AgentAccess) Queries() queries.Store { return a.ws.Queries() }

// Skills is the workspace's skills/ folder.
func (a *AgentAccess) Skills() skills.Store { return a.ws.Skills() }

// Artifacts is the workspace's artifacts/ folder.
func (a *AgentAccess) Artifacts() artifacts.Store { return a.ws.Artifacts() }

// Semantics is the workspace's semantics/ folder, read only: the agent's tools
// are given no way to write a definition (accepting a proposal is the person's).
func (a *AgentAccess) Semantics() semantics.ReadStore {
	return struct{ semantics.ReadStore }{a.ws.Semantics()}
}

// Proposals is the inbox of proposed definitions as a proposer sees it: it can
// add a proposal and count them, and cannot read, accept, reject or change one.
func (a *AgentAccess) Proposals() proposals.Proposer {
	return struct{ proposals.Proposer }{a.ws.Proposals()}
}

// AgentPolicy is the workspace's effective policy for this person.
func (a *AgentAccess) AgentPolicy() (string, bool) { return a.ws.AgentPolicy() }

// ProtectedPaths are the files and folders of this workspace's local
// connections (a DuckLake's catalog and data folder, duckdb and sqlite files):
// the agent's COPY ... TO never writes into them, whatever they are called.
func (a *AgentAccess) ProtectedPaths() []string {
	var out []string
	for _, c := range a.ws.Connections() {
		rc, err := ResolveConnection(a.ws, c, nil)
		if err != nil {
			continue
		}
		if usesLocalPath(rc.Resolved) && rc.Path != "" {
			out = append(out, rc.Path)
		}
		if dp := rc.Resolved.Options["data_path"]; rc.Resolved.Type == "ducklake" && dp != "" && !connections.IsDataURL(dp) {
			out = append(out, dp)
		}
	}
	return out
}
