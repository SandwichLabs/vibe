// Package skills embeds Vibe's built-in Skills (#62): SKILL.md documents
// bundled into the binary from internal/llm/skills/<name>/SKILL.md, one
// directory per skill, agentskills.io-style. This package has no dependency
// on internal/agents or internal/llm — it just parses frontmatter and
// exposes a Skill list — so internal/agents can import it (to merge built-ins
// with the user skills stored in _vibe_skills) without creating an import
// cycle (internal/llm already imports internal/agents).
//
// A sibling ADDENDUM.md next to a SKILL.md, if present, is a human's
// hand-written notes appended under "## Addendum (hand-written)" at load
// time — mirroring ../../../../schoolwork-workspace/src/agent/skills.js. No
// built-in ships one today; the mechanism exists so a future skill can carry
// operator notes without editing the generated/authored SKILL.md body.
package skills

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// NamePattern is the required shape for every skill name — built-in or
// user-authored: lowercase kebab-case, 1-64 characters. User skills are
// validated against this in internal/agents; built-ins are asserted against
// it at init time (a broken embedded SKILL.md is a build-time bug, not a
// runtime one).
var NamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// TaskPrefix marks a skill as belonging to notebook task-mode authoring
// (#66) only: hidden from chat's system prompt and from what chat's
// read_skill/list_skills will return, but still a real capability once task
// mode exists. No built-in skill uses this prefix yet (task mode doesn't
// exist until #66) — the constant exists now so the filtering logic and its
// tests are in place ahead of that.
const TaskPrefix = "task-"

// Skill is one skill's normalized form: routing name, one-line description
// (what the system prompt's <skills> block advertises), and full markdown
// instructions (fetched lazily via read_skill).
type Skill struct {
	Name         string
	Description  string
	Instructions string
}

//go:embed */*.md
var embedded embed.FS

// Builtins returns every embedded built-in skill, sorted by name. Parsed
// once (not cached) so a future hot-reload story has nothing to invalidate;
// the embed.FS read is cheap and this is called at most once per chat turn.
func Builtins() []Skill {
	entries, err := embedded.ReadDir(".")
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		raw, err := embedded.ReadFile(dir + "/SKILL.md")
		if err != nil {
			continue // a directory with no SKILL.md isn't a skill.
		}
		name, description, body, ok := ParseFrontmatter(string(raw))
		if !ok {
			continue
		}
		if !NamePattern.MatchString(name) {
			panic(fmt.Sprintf("built-in skill %q has an invalid name %q (must match %s)", dir, name, NamePattern.String()))
		}
		if addendum, err := embedded.ReadFile(dir + "/ADDENDUM.md"); err == nil {
			if trimmed := strings.TrimSpace(string(addendum)); trimmed != "" {
				body = body + "\n\n## Addendum (hand-written)\n\n" + trimmed
			}
		}
		out = append(out, Skill{Name: name, Description: description, Instructions: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseFrontmatter parses a SKILL.md document: YAML-flavored frontmatter
// between a pair of `---` lines (only simple `key: value` pairs are
// supported — the same minimal subset schoolwork-workspace's parser
// handles), followed by the markdown body. Both "name" and "description"
// are required; a missing pair, malformed frontmatter, or missing closing
// delimiter returns ok=false. CRLF line endings are normalized first so a
// file edited on Windows still parses.
func ParseFrontmatter(raw string) (name, description, body string, ok bool) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return "", "", "", false
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", "", false
	}
	frontmatter := rest[:end]
	after := rest[end+len("\n---"):]
	after = strings.TrimPrefix(after, "\n")

	meta := map[string]string{}
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	name = meta["name"]
	description = meta["description"]
	if name == "" || description == "" {
		return "", "", "", false
	}
	return name, description, strings.TrimSpace(after), true
}
