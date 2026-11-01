package semantics

import (
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/assay/internal/strictyaml"
)

// MaxTermText caps the body of one glossary term a proposal carries.
const MaxTermText = 8 << 10

// MaxTerm caps a glossary term (the heading text), in characters.
const MaxTerm = 100

// glossarySection is one "## term" section of glossary.md: the byte span from
// its heading line to the next heading (or the end).
type glossarySection struct {
	term       string
	start, end int
}

// scanGlossary finds the "## " headings outside fenced code blocks. openFence
// reports a code fence left open at the end, which would swallow whatever is
// appended after it.
func scanGlossary(text string) (secs []glossarySection, openFence bool) {
	fence := ""
	off := 0
	for _, raw := range strings.SplitAfter(text, "\n") {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			switch marker := trimmed[:3]; {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
		case fence != "":
		default:
			if m := glossaryHeadingRE.FindStringSubmatch(line); m != nil {
				if n := len(secs); n > 0 {
					secs[n-1].end = off
				}
				secs = append(secs, glossarySection{term: m[1], start: off})
			}
		}
		off += len(raw)
	}
	if n := len(secs); n > 0 {
		secs[n-1].end = len(text)
	}
	return secs, fence != ""
}

func findTerm(secs []glossarySection, term string) *glossarySection {
	for i := range secs {
		if strings.EqualFold(secs[i].term, term) {
			return &secs[i]
		}
	}
	return nil
}

// GlossarySection returns the body of term's "## term" section (the text under
// the heading, trimmed), matching the term without regard to case.
func GlossarySection(text, term string) (string, bool) {
	secs, _ := scanGlossary(text)
	s := findTerm(secs, strings.TrimSpace(term))
	if s == nil {
		return "", false
	}
	block := text[s.start:s.end]
	if i := strings.IndexByte(block, '\n'); i >= 0 {
		block = block[i+1:]
	} else {
		block = ""
	}
	return strings.TrimSpace(block), true
}

// ValidateGlossaryTerm checks a term and its text as a proposal carries them:
// the term is one line that reads back as the same heading, the text is bounded,
// free of hidden characters and cannot start another term (a "## " line) or
// leave a code fence open.
func ValidateGlossaryTerm(term, body string) error {
	if strings.TrimSpace(term) == "" {
		return invalid("term is required")
	}
	if term != strings.TrimSpace(term) || strings.ContainsAny(term, "\r\n") {
		return invalid("term must be one line without leading or trailing spaces")
	}
	if n := utf8.RuneCountInString(term); n > MaxTerm {
		return invalid("term is %d characters, over the %d character limit", n, MaxTerm)
	}
	if err := noHidden("term", term, true); err != nil {
		return err
	}
	if m := glossaryHeadingRE.FindStringSubmatch("## " + term); m == nil || m[1] != term {
		return invalid("term %q would not read back as the same heading (no trailing # marks)", term)
	}
	if strings.TrimSpace(body) == "" {
		return invalid("text is required")
	}
	if len(body) > MaxTermText {
		return invalid("text is %d bytes, over the %d byte limit", len(body), MaxTermText)
	}
	if r, ok := strictyaml.FirstHiddenRune(body, false); ok {
		return invalid("text contains a control character (U+%04X)", r)
	}
	secs, open := scanGlossary("## " + term + "\n\n" + body + "\n")
	if open {
		return invalid("text leaves a code fence open")
	}
	if len(secs) != 1 {
		return invalid("text may not contain a level-2 heading (a line starting with \"## \"): that would start another term")
	}
	return nil
}

// GlossaryOpenFence reports a glossary whose code fence is never closed: a
// section appended after it would be swallowed by the fence and not be a term.
func GlossaryOpenFence(text string) bool {
	_, open := scanGlossary(text)
	return open
}

// HasGlossaryTerm reports whether the glossary defines term (without regard to case).
func HasGlossaryTerm(text, term string) bool {
	secs, _ := scanGlossary(text)
	return findTerm(secs, strings.TrimSpace(term)) != nil
}

// SetGlossaryTerm returns the glossary with term's section set to body: the
// existing section replaced in place (matching the term without regard to
// case, spelled as given), or a new one appended. replaced says which. The
// rest of the file is untouched, and a glossary written with CRLF line endings
// keeps them. Call ValidateGlossaryTerm first, and refuse GlossaryOpenFence.
func SetGlossaryTerm(text, term, body string) (out string, replaced bool) {
	crlf := strings.Contains(text, "\r\n") && strings.Count(text, "\r\n") == strings.Count(text, "\n")
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	out, replaced = setGlossaryTerm(text, term, body)
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out, replaced
}

func setGlossaryTerm(text, term, body string) (string, bool) {
	section := "## " + term + "\n\n" + strings.TrimSpace(body) + "\n"
	secs, _ := scanGlossary(text)
	if s := findTerm(secs, term); s != nil {
		rest := text[s.end:]
		if rest != "" {
			section += "\n"
		}
		return text[:s.start] + section + rest, true
	}
	switch {
	case strings.TrimSpace(text) == "":
		return section, false
	case strings.HasSuffix(text, "\n\n"):
		return text + section, false
	case strings.HasSuffix(text, "\n"):
		return text + "\n" + section, false
	}
	return text + "\n\n" + section, false
}
