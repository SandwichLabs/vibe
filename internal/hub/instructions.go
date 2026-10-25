package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/agents"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// maxInstructionConnections caps the connection names listed in the server
// instructions; a client may truncate them (Claude Code stops at 2,048
// characters), so the text stays short and points at list_connections.
const maxInstructionConnections = 12

// instructionsInput is everything the server instructions say. It holds
// names, paths and settings only: no credential, URL or token.
type instructionsInput struct {
	Workspace   string
	Root        string
	Policy      agents.FilePolicy
	HasApprover bool
	Connections []string
}

// mcpServerOptions are the vibemcp options both MCP paths (headless stdio and
// the SSE server) use, so they report the same identity and orientation.
func mcpServerOptions(ws workspace.Workspace, sess session.Session, h *agents.Handler, app, version string, refresh func()) []vibemcp.Option {
	if app == "" {
		app = AppServe
	}
	opts := []vibemcp.Option{vibemcp.WithServerInfo(app, version)}
	if h == nil {
		return opts
	}
	return append(opts, vibemcp.WithInstructions(func(ctx context.Context) string {
		if refresh != nil {
			// Studio re-reads the saved settings into the handler here, so a
			// client that connects after a change is told the current state.
			refresh()
		}
		return instructionsText(instructionsInput{
			Workspace:   shortName(ws.Name()),
			Root:        ws.Root(),
			Policy:      h.Policy(ctx),
			HasApprover: h.HasApprover(),
			Connections: connectionNames(ws, sess),
		})
	}))
}

// connectionNames lists the connection names (the catalog aliases) the agent
// can use, from the live session when there is one.
func connectionNames(ws workspace.Workspace, sess session.Session) []string {
	var names []string
	if sess != nil {
		for _, c := range sess.ConnectionInfos() {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		for _, c := range ws.Connections() {
			names = append(names, c.Name)
		}
	}
	return names
}

// instructionsText renders the short orientation sent in initialize. The gate
// state is the state when the client connected: the instructions are sent once,
// so a setting the person changes later is not reflected until a client
// reconnects.
func instructionsText(in instructionsInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Vibe workspace %q at %s.\n", in.Workspace, in.Root)
	fmt.Fprintf(&b, "At connect time the agent's sandbox was %s (%q) and \"Ask before changes\" was %s. %s\n",
		in.Policy.Mode, agents.TierLabel(in.Policy.Mode), onOff(in.Policy.AskBeforeWrites), gateSentence(in))
	fmt.Fprintf(&b, "Files: file readers only read inside %s (use absolute paths; relative paths are refused).\n",
		filepath.Join(in.Root, "data")+string(filepath.Separator))
	fmt.Fprintf(&b, "Connections (a name is the catalog in SQL): %s.\n", connectionList(in.Connections))
	b.WriteString("Start with help_search \"\" for an index of every topic, and read_skill duckdb-sql and data-artifacts before writing SQL or artifacts, and a2ui before present_data, create_dashboard or an artifact view.\n")
	b.WriteString("Settings can change while you are connected; this text is from connect time.")
	return b.String()
}

// maxInstructionName caps the workspace name, which comes from a committed
// vibe.yaml and so is not ours to trust for length or content.
const maxInstructionName = 60

func shortName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, name)
	if r := []rune(name); len(r) > maxInstructionName {
		return string(r[:maxInstructionName]) + "..."
	}
	return name
}

func gateSentence(in instructionsInput) string {
	switch {
	case in.Policy.Mode == agents.ModeReadOnly:
		return "Nothing can be changed. To allow changes the person sets the agent to \"Change rows\" or higher in " + agents.SettingsTierPath +
			"; a --sandbox flag on `vibe mcp serve` or a team limit in vibe.yaml can cap it lower."
	case in.Policy.AskBeforeWrites && in.HasApprover:
		return "Each change shows the person an approval card in Vibe Studio and waits for their OK."
	case in.Policy.AskBeforeWrites:
		return "Writes need the person's OK and nobody can give it over this connection, so they are refused. " +
			"write_artifact, the notebook cell tools (add_query_cell, add_markdown_cell, update_cell, delete_cell, move_cell, create_dashboard), save_query, build_query and create_or_update_skill check your request first: when one says it is valid and ready, ask the person to approve it, or to turn off " +
			agents.SettingsAskPath + "; that applies when the MCP server restarts."
	}
	return "Changes to this workspace's own local files run without asking; any other connection always needs the person's approval in Vibe Studio."
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func connectionList(names []string) string {
	if len(names) == 0 {
		return "none yet (list_connections)"
	}
	if len(names) > maxInstructionConnections {
		return strings.Join(names[:maxInstructionConnections], ", ") + fmt.Sprintf(", and %d more (list_connections)", len(names)-maxInstructionConnections)
	}
	return strings.Join(names, ", ")
}
