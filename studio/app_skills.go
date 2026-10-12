package main

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/skills"
)

// --- Skills (#62) ---

// SkillSummary is one skill as the Skills view lists it.
type SkillSummary struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	TableName    string `json:"table_name,omitempty"`
	Source       string `json:"source"` // built-in | user | import
	// Rev is the store's revision as listed; SaveSkill sends it back so a
	// save from a stale copy is a conflict (file-backed workspaces).
	Rev string `json:"rev,omitempty"`
}

// ListSkills returns every skill visible to chat: built-ins plus stored
// user/import skills that pass table gating (a table-bound skill only
// appears once its table exists and has rows). Mirrors exactly what the
// agent's list_skills tool would return, so the Skills view shows the same
// set the model can actually see.
func (a *App) ListSkills() ([]SkillSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	entries, err := skills.LoadSkills(a.ws.Skills(), a.sess, a.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}
	visible := skills.ChatVisibleSkills(entries)
	out := make([]SkillSummary, 0, len(visible))
	for _, s := range visible {
		out = append(out, SkillSummary{
			Name: s.Name, Description: s.Description, Instructions: s.Instructions,
			TableName: s.TableName, Source: s.Source, Rev: s.Rev,
		})
	}
	return out, nil
}

// SaveSkill creates or updates a user-authored skill. Studio-only; rejects
// built-in names, the reserved "task-" prefix, and invalid names — the same
// validation the create_or_update_skill agent tool applies (SaveUserSkillAt is
// shared by both entry points). Like page/table CRUD elsewhere in this file,
// this bypasses the agent sandbox gate entirely: it's the user directly
// authoring content in Studio, not an agent tool call.
func (a *App) SaveSkill(name, description, instructions, tableName, rev string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if _, err := skills.SaveUserSkillAt(a.ws.Skills(), a.sess, name, description, instructions, tableName, rev); err != nil {
		return conflictErr(err)
	}
	a.emitEvent("data:changed")
	return nil
}

// DeleteSkill removes a user- or import-authored skill. Built-ins can't be
// deleted (they aren't rows in _vibe_skills).
func (a *App) DeleteSkill(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := skills.DeleteSkill(a.ws.Skills(), name); err != nil {
		return err
	}
	a.emitEvent("data:changed")
	return nil
}
