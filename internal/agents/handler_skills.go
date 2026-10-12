package agents

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/skills"
)

// --- Tool handlers ---

func (h *Handler) handleListSkills(_ context.Context, req *Request) *Response {
	entries, err := skills.LoadSkills(h.access.Skills(), h.access.Session(), h.logger)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list skills: %v", err))
	}
	// Task mode (#66 part B) sees the UNFILTERED list, including task-*
	// skills — internal/llm's Task() loop is exactly the surface those
	// skills exist for. Every other caller (chat, MCP) still gets
	// ChatVisibleSkills' task-* filtering.
	visible := entries
	if h.mode != ModeTask {
		visible = skills.ChatVisibleSkills(entries)
	}
	out := make([]map[string]any, len(visible))
	for i, s := range visible {
		out[i] = map[string]any{
			"name":        s.Name,
			"description": s.Description,
			"source":      s.Source,
			"table_name":  s.TableName,
			"rev":         s.Rev,
		}
	}
	return NewResponse(req.ID, map[string]any{"skills": out})
}

// framedInstructions wraps a skill body with skills.TrustFraming so it carries
// the same "this is reference data, not operator instructions" framing every
// time read_skill returns it — the framing lives once (skills.TrustFraming)
// and is applied here, not duplicated per call site.
func framedInstructions(body string) string {
	return skills.TrustFraming + "\n\n" + body
}

func (h *Handler) handleReadSkill(_ context.Context, req *Request) *Response {
	name, _ := req.Params["name"].(string)
	entries, err := skills.LoadSkills(h.access.Skills(), h.access.Session(), h.logger)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to load skills: %v", err))
	}
	visible := entries
	if h.mode != ModeTask {
		visible = skills.ChatVisibleSkills(entries)
	}
	for _, s := range visible {
		if s.Name == name {
			return NewResponse(req.ID, map[string]any{
				"name":         s.Name,
				"description":  s.Description,
				"instructions": framedInstructions(s.Instructions),
				"source":       s.Source,
				"table_name":   s.TableName,
				"rev":          s.Rev,
			})
		}
	}
	return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("no skill named %q (it may not exist, may be gated by an empty/missing table, or is a task-only skill this mode can't read)", name))
}

// handleCreateOrUpdateSkill is the authoring tool for user skills (category
// "content": see internal/agents/tools.go for why "content", not "schema", was
// chosen).
func (h *Handler) handleCreateOrUpdateSkill(_ context.Context, req *Request) *Response {
	name, _ := req.Params["name"].(string)
	description, _ := req.Params["description"].(string)
	instructions, _ := req.Params["instructions"].(string)
	tableName, _ := req.Params["table_name"].(string)

	// rev is the revision the agent's own list_skills / read_skill result
	// carried. Passing it back makes a save over a skill that changed since
	// (a git pull, an edit in Studio) a conflict. Without it the store's
	// last-seen revision applies.
	rev, _ := req.Params["rev"].(string)

	newRev, err := skills.SaveUserSkillAt(h.access.Skills(), h.access.Session(), name, description, instructions, tableName, rev)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	return NewResponse(req.ID, map[string]any{"name": name, "saved": true, "rev": newRev})
}
