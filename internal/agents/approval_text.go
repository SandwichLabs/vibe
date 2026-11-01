package agents

import (
	"fmt"
	"strings"
)

// The words an agent is given when a call is refused for want of a tier or an
// approval. They name where the person changes it, so the agent can pass the
// instruction on instead of guessing. The paths match studio/frontend's
// AgentSettings.vue; keep them in step with it.
const (
	// SettingsAgentPath is the Studio section that holds the agent's tier and
	// the ask-before-changes switch.
	SettingsAgentPath = "Slaw → Settings → Workspace → The agent"
	// SettingsTierPath is the tier picker, in the "On this computer" half.
	SettingsTierPath = SettingsAgentPath + " → On this computer"
	// SettingsAskPath names the ask-before-changes gate and where its switch is.
	SettingsAskPath = `"Ask before changes" (the switch "Ask me before it changes data" in ` + SettingsTierPath + `)`
)

// TierLabel is the Studio button text of a tier ("Look only" for read-only).
func TierLabel(m SandboxMode) string {
	switch m {
	case ModeReadOnly:
		return "Look only"
	case ModeReadWrite:
		return "Change rows"
	case ModeFull:
		return "Full control"
	}
	return string(m)
}

// tierOrder lists the tiers from the most restrictive up.
var tierOrder = []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull}

// tierNeeded is the lowest tier whose categories include category, read from
// categoryPermissions so the rule lives in one place.
func tierNeeded(category string) SandboxMode {
	for _, m := range tierOrder {
		if categoryPermissions[m][category] {
			return m
		}
	}
	return ModeFull
}

// categoryWhat says in plain words what a category of tool changes.
func categoryWhat(category string) string {
	switch category {
	case "content":
		return "writes files in the workspace folder (artifacts, notebooks, skills, saved queries)"
	case "data":
		return "changes rows in a connection"
	case "schema":
		return "changes the structure of a connection's tables"
	}
	return "changes the workspace"
}

// tierFix is the sentence that says how to raise the tier to need.
func tierFix(need SandboxMode) string {
	return fmt.Sprintf(`To allow it, set the agent to "%s" (%s) or higher in %s, or restart the MCP server without a tighter --sandbox. A team limit saved in slaw.yaml can cap the tier too.`,
		TierLabel(need), need, SettingsTierPath)
}

// tierRefusal is the message of a tool the current tier does not allow.
func tierRefusal(method, category string, mode SandboxMode) string {
	need := tierNeeded(category)
	return fmt.Sprintf(`method %q (category %q) is not allowed in %s mode: it %s, which needs the %s tier ("%s"), and this workspace's agent is at %s ("%s"). Nothing was changed. %s`,
		method, category, mode, categoryWhat(category), need, TierLabel(need), mode, TierLabel(mode), tierFix(need))
}

// contentTarget names what a content tool writes, for the refusal text.
func contentTarget(req *Request) string {
	switch req.Method {
	case "write_artifact":
		return "artifacts/" + nameOr(req, "<name>") + "/ in the workspace folder"
	case "create_or_update_skill":
		return "skills/" + nameOr(req, "<name>") + "/SKILL.md in the workspace folder"
	case "save_query", "build_query":
		return "a saved query in queries/ in the workspace folder"
	case "propose_definition":
		return "a proposal in .slaw-local/proposals/ in the workspace folder (nothing in semantics/ changes until the person accepts it)"
	default:
		return "a notebook in the workspace folder"
	}
}

func nameOr(req *Request, fallback string) string {
	if n := strings.TrimSpace(stringParam(req.Params["name"])); n != "" {
		return n
	}
	return fallback
}

// approvalNeededData is the structured payload of a content refusal: the
// request passed validation and only the person's OK is missing.
func approvalNeededData() map[string]any {
	return map[string]any{"valid": true, "needs_approval": true, "nothing_written": true}
}

// contentNeedsApproval is the answer to a valid content write that needs an OK
// nobody can give here (no approver: headless MCP).
func contentNeedsApproval(req *Request, sb *SandboxConfig) *Response {
	resp := NewErrorResponse(req.ID, ErrApprovalNeeded, fmt.Sprintf(
		"%s: your request is valid and ready, but it was NOT saved yet. It writes %s, and this workspace asks before the agent changes anything (sandbox: %s), with no one here to approve over MCP. "+
			"Nothing was written. Ask the person to approve it: they can make the same change from Slaw's agent chat, or with Studio's MCP server on (`slaw mcp serve` passes through to it) it shows them an approval card; or they can turn off %s; that applies when the MCP server restarts (reconnect the client, or restart `slaw mcp serve`), so send the same call again after that.",
		req.Method, contentTarget(req), sb.Mode, SettingsAskPath))
	resp.Error.Data = approvalNeededData()
	return resp
}

// dataNeedsApproval is the answer to a data or schema write that needs an OK
// nobody can give here.
func dataNeedsApproval(req *Request, sb *SandboxConfig) *Response {
	return NewErrorResponse(req.ID, ErrApprovalNeeded, fmt.Sprintf(
		"%s needs your OK before it runs: it changes data in the workspace's local database, and this workspace asks before the agent changes anything (sandbox: %s), with no one here to approve over MCP. "+
			"Nothing was changed. Changes to any connection other than the workspace's local database always need Slaw: open the workspace there and approve the change. "+
			"To let MCP clients change the local database without asking, turn off %s; it applies when the MCP server restarts (reconnect the client, or restart `slaw mcp serve`).",
		req.Method, sb.Mode, SettingsAskPath))
}
