package artifacts

import (
	"regexp"
	"sort"
	"strings"
)

// entryRe is the shape of an entry template name: a plain file name, directly
// under html/, ending in .tmpl.
var entryRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,99}\.tmpl$`)

// validateHTMLSpec checks the html: block of a manifest by itself (the file
// set is checked by VetPage): it belongs only to view: html, the entry is a
// plain .tmpl name, a window is within bounds and a capability is one of the
// closed set with a setting it knows. Whether the page is scripted, which
// decides whether it may have a window or capabilities, is derived from the
// files, not declared.
func validateHTMLSpec(m *Manifest) error {
	if m.View != ViewHTML {
		if m.HTML != nil {
			return invalid("html: is only for a manifest with view: %s", ViewHTML)
		}
		return nil
	}
	if m.HTML == nil {
		m.HTML = &HTMLSpec{}
	}
	h := m.HTML
	if h.Entry == "" {
		h.Entry = DefaultEntry
	}
	if !entryRe.MatchString(h.Entry) {
		return invalid("html.entry %q must be a .tmpl file directly under %s/ (letters, digits, . _ -)", h.Entry, HTMLDir)
	}
	if w := h.Window; w != nil {
		for _, d := range []struct {
			name string
			v    int
		}{{"width", w.Width}, {"height", w.Height}} {
			if d.v < MinWindowSize || d.v > MaxWindowSize {
				return invalid("html.window.%s must be %d to %d", d.name, MinWindowSize, MaxWindowSize)
			}
		}
	}
	for name, c := range h.Capabilities {
		if err := validateCapability(name, c); err != nil {
			return err
		}
	}
	return nil
}

func validateCapability(name string, c Capability) error {
	switch name {
	case CapState:
		if c.Scope != "" && c.Scope != "local" {
			return invalid("html.capabilities.state.scope %q is not supported: only %q (a shared scope is not built)", c.Scope, "local")
		}
	case CapDownload, CapAsk:
		if c.Scope != "" {
			return invalid("html.capabilities.%s takes no settings", name)
		}
	default:
		return invalid("html.capabilities: %q is not a capability (the closed set is %s)", name, strings.Join([]string{CapState, CapDownload, CapAsk}, ", "))
	}
	return nil
}

// CapabilityNames lists a manifest's declared capabilities, sorted.
func CapabilityNames(m *Manifest) []string {
	if m.HTML == nil {
		return nil
	}
	out := make([]string, 0, len(m.HTML.Capabilities))
	for n := range m.HTML.Capabilities {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
