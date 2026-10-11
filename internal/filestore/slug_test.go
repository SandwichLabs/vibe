package filestore

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The four slug helpers SlugOr replaced, kept verbatim so each old caller's
// output is pinned: filestore.Slug itself, hub's slug, studio's workspaceSlug
// and agents' importSkillSlug.
var legacyNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func legacyFilestoreSlug(title string) string {
	s := strings.Trim(legacyNonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	if s == "" {
		return "untitled"
	}
	return s
}

func legacyHubSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workspace"
	}
	return out
}

func legacyWorkspaceSlug(name string) string {
	s := strings.Trim(legacyNonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if s == "" {
		return "workspace"
	}
	return s
}

func legacyImportSkillSlug(table string) string {
	slug := legacyNonSlug.ReplaceAllString(strings.ToLower(table), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "table"
	}
	return slug
}

var slugCorpus = []string{
	"", "   ", "plain", "Acme  Analytics!", "???", "--lead-and-trail--", "UPPER_snake_case", "café", "日本語",
	"order_items", "a  b\tc\nd", "x.y/z", strings.Repeat("x", 100), strings.Repeat("ab-", 40), "İstanbul", "9lives",
	"  Padded Name  ", strings.Repeat("-", 5) + "z", strings.Repeat("a", 64), strings.Repeat("a", 63) + "-b",
}

func TestSlugOr_MatchesEveryLegacyCaller(t *testing.T) {
	for _, in := range slugCorpus {
		assert.Equal(t, legacyFilestoreSlug(in), Slug(in), "Slug %q", in)
		assert.Equal(t, legacyHubSlug(in), SlugOr(in, "workspace", 0), "hub %q", in)
		assert.Equal(t, legacyWorkspaceSlug(in), SlugOr(strings.TrimSpace(in), "workspace", 0), "workspace %q", in)
		assert.Equal(t, legacyImportSkillSlug(in), SlugOr(in, "table", 0), "import skill %q", in)
	}
}
