// Package strictyaml is the one strict YAML decoder behind every document Assay
// reads from a folder somebody else may have written: artifact.yaml and the
// semantics/ files. A leaf (yaml.v3 and the standard library only), so no store
// has a decoder of its own to drift from.
//
// Decode refuses, in this order: a document over the byte cap (checked before
// anything is parsed), a control character, a tree over the node cap, an
// anchor, an alias, an explicit tag or a merge key (they exist only to
// multiply a small document into a large one, or to hide what a value is), an
// unknown or duplicate key, an empty document and a second document. Nothing
// is interpolated.
package strictyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Error is a structural refusal. Its text is the message without any package
// prefix, so a caller wraps it in its own sentinel ("invalid artifact: ...").
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func refuse(format string, args ...any) error {
	// The message may echo document text (a YAML parser quotes the value it
	// choked on), so it is made safe to print.
	return &Error{Msg: EscapeHidden(fmt.Sprintf(format, args...))}
}

// Decode parses data into into under the rules in the package comment. label
// names the document in messages ("artifact.yaml"). Every refusal is an *Error.
func Decode(data []byte, maxBytes, maxNodes int, label string, into any) error {
	if len(data) > maxBytes {
		return refuse("%s is %d bytes, over the %d byte limit", label, len(data), maxBytes)
	}
	if r, ok := FirstHiddenRune(string(data), false); ok {
		return refuse("%s contains a control character (U+%04X)", label, r)
	}
	if err := checkShape(data, maxNodes, label); err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		if errors.Is(err, io.EOF) {
			return refuse("%s is empty", label)
		}
		return refuse("%s: %v%s", label, err, flowHint(err.Error(), data))
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return refuse("%s must hold a single YAML document", label)
	}
	return nil
}

const flowHintText = "; inside {...} quote a value that contains , ( ) : or #"

var (
	flowLineRe  = regexp.MustCompile(`line (\d+):`)
	flowFieldRe = regexp.MustCompile(`field (\S+) not found`)
)

// flowHint explains the usual cause of a flow-mapping failure: an unquoted
// value such as (key, unit) inside {...} that YAML splits at the comma, which
// shows up as a parse error ("did not find expected ',' or '}'") or as an
// unknown field named "unit)". It returns "" for any other error, and when no
// line the message points at holds a "{".
func flowHint(msg string, data []byte) string {
	parse := strings.Contains(msg, "did not find expected ',' or '}'") || strings.Contains(msg, "did not find expected key")
	split := false
	if m := flowFieldRe.FindStringSubmatch(msg); m != nil {
		split = strings.ContainsAny(m[1], "()")
	}
	if !parse && !split {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	nums := flowLineRe.FindAllStringSubmatch(msg, -1)
	if len(nums) == 0 {
		// The parser sometimes reports no line; look at the whole document.
		if strings.Contains(string(data), "{") {
			return flowHintText
		}
		return ""
	}
	for _, m := range nums {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= len(lines) && strings.Contains(lines[n-1], "{") {
			return flowHintText
		}
	}
	return ""
}

// checkShape parses into the node tree (which never expands aliases) and
// refuses anchors, aliases, tags, merge keys and an oversized tree.
func checkShape(data []byte, maxNodes int, label string) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return refuse("%s: %v%s", label, err, flowHint(err.Error(), data))
	}
	count := 0
	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		count++
		if count > maxNodes {
			return refuse("%s has too many YAML nodes (limit %d)", label, maxNodes)
		}
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return refuse("%s may not use YAML anchors or aliases", label)
		}
		if n.Style&yaml.TaggedStyle != 0 {
			return refuse("%s may not use explicit YAML tags", label)
		}
		if n.Kind == yaml.ScalarNode && n.Tag == "!!merge" {
			return refuse("%s may not use YAML merge keys", label)
		}
		for _, c := range n.Content {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(&root)
}

// FirstHiddenRune finds a character that can make shown text differ from what
// is stored: C0/C1 controls (tab, newline and carriage return excepted), DEL,
// the bidirectional overrides and isolates, and the line and paragraph
// separators. With invisible it also finds zero-width and other invisible
// formatting characters.
func FirstHiddenRune(s string, invisible bool) (rune, bool) {
	for _, r := range s {
		switch {
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x2028, r == 0x2029:
			return r, true
		case invisible && (r == 0x200e || r == 0x200f || (r >= 0x200b && r <= 0x200d) || r == 0x2060 || r == 0xfeff ||
			r == 0x00ad || r == 0x061c || (r >= 0xfff9 && r <= 0xfffc)):
			return r, true
		}
	}
	return 0, false
}

// EscapeHidden shows s with every character FirstHiddenRune(s, true) would
// find, and every carriage return (which can overwrite a line on a terminal),
// replaced by a visible <U+XXXX> marker. For printing text that came from a
// file somebody else may have written.
func EscapeHidden(s string) string {
	var b strings.Builder
	for _, r := range s {
		if _, hidden := FirstHiddenRune(string(r), true); hidden || r == '\r' {
			fmt.Fprintf(&b, "<U+%04X>", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
