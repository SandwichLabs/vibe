package agents

import (
	"context"
	"errors"
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
				"framing":      skills.TrustFraming,
				"instructions": s.Instructions,
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
func (h *Handler) handleCreateOrUpdateSkill(ctx context.Context, req *Request) *Response {
	name, _ := req.Params["name"].(string)
	description, _ := req.Params["description"].(string)
	instructions, _ := req.Params["instructions"].(string)
	tableName, _ := req.Params["table_name"].(string)
	// A model may paste read_skill's framing back into the body; it is
	// prompt framing, not skill content, so it never reaches SKILL.md.
	instructions = skills.StripTrustFraming(instructions)

	// rev is the revision the agent's own list_skills / read_skill result
	// carried. Passing it back makes a save over a skill that changed since
	// (a git pull, an edit in Studio) a conflict. Without it the write only
	// creates: WithRev("") keeps the store's last-seen revision from standing
	// in, so the agent cannot replace a skill it never read (#166).
	rev, _ := req.Params["rev"].(string)

	if isDryRun(ctx) {
		return h.skillDryRun(ctx, req.ID, name, description, instructions, tableName, rev)
	}

	newRev, err := skills.SaveUserSkillAt(h.access.Skills(), h.access.Session(), name, description, instructions, tableName, rev, skills.WithRev(rev))
	if err != nil {
		if errors.Is(err, skills.ErrConflict) {
			return NewErrorResponse(req.ID, ErrInvalidParams, skillConflictMessage(name, rev))
		}
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	return NewResponse(req.ID, map[string]any{"name": name, "saved": true, "rev": newRev})
}

// skillDryRun is the end of a dry run: the same checks the write makes, then
// the conflict the write would hit (an existing skill without its rev, or a
// stale rev), with nothing written.
func (h *Handler) skillDryRun(ctx context.Context, id any, name, description, instructions, tableName, rev string) *Response {
	if err := skills.ValidateUserSkill(h.access.Session(), name, description, instructions, tableName); err != nil {
		return NewErrorResponse(id, ErrInvalidParams, err.Error())
	}
	cur, err := h.access.Skills().Get(ctx, name)
	switch {
	case err == nil && (rev == "" || cur.Rev != rev):
		return NewErrorResponse(id, ErrInvalidParams, skillConflictMessage(name, rev))
	case errors.Is(err, skills.ErrNotFound):
		if rev != "" {
			return NewErrorResponse(id, ErrInvalidParams, skillConflictMessage(name, rev))
		}
	case err != nil:
		return NewErrorResponse(id, ErrInvalidParams, fmt.Sprintf("skill %q is stored but cannot be read, so nothing was checked as ready: %s", name, scrubStoreText(err.Error())))
	}
	return dryRunOK(id)
}

// skillConflictMessage tells the agent how to recover from a refused write:
// read the skill, then send the rev it returned.
func skillConflictMessage(name, rev string) string {
	if rev == "" {
		return fmt.Sprintf("a skill named %q already exists, so nothing was written. Call read_skill to read it, then send its \"rev\" with your changes (or pick another name). A skill bound to a table is visible to read_skill and list_skills only while that table has rows.", name)
	}
	return fmt.Sprintf("skill %q changed since the rev you sent (a person or another tool edited it) or no longer exists, so nothing was written. Call read_skill again, merge your change into what it returns, and send the new \"rev\".", name)
}
