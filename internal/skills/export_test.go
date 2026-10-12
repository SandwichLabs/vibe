package skills

import "github.com/SandwichLabs/vibe/internal/session"

// SaveUserSkill is SaveUserSkillAt without a revision check: the shape most
// of this package's tests want.
func SaveUserSkill(store Store, sess session.Session, name, description, instructions, tableName string) error {
	_, err := SaveUserSkillAt(store, sess, name, description, instructions, tableName, "")
	return err
}

// SkillsPromptBlock renders the <skills> system-prompt block for a slice of
// full SkillEntry values (name + description only). Production renders it from
// the list_skills result via RenderSkillsBlock; this wraps that for tests.
func SkillsPromptBlock(entries []SkillEntry) string {
	visible := ChatVisibleSkills(entries)
	refs := make([]SkillRef, len(visible))
	for i, s := range visible {
		refs[i] = SkillRef{Name: s.Name, Description: s.Description}
	}
	return RenderSkillsBlock(refs)
}
