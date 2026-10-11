package notebooks

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/jsonenc"
)

// Extension is the notebook file extension.
const Extension = ".vnb"

// Dir is the workspace-relative directory notebooks live in.
const Dir = "notebooks"

// fileDoc is the on-disk shape of a .vnb file. It deliberately omits id (the
// file stem is the id, so a `git mv` is a rename and nothing inside the file
// can disagree with it), updatedAt (the file's mtime; a timestamp inside the
// file would dirty the diff on every save) and rev (derived from the bytes).
// Field order is the key order, which is what keeps diffs small and the
// save -> load -> save cycle byte-identical.
type fileDoc struct {
	SchemaVersion int    `json:"schemaVersion"`
	Title         string `json:"title"`
	Cells         []Cell `json:"cells"`
}

// EncodeFile renders a notebook as .vnb bytes: pretty JSON, 2-space indent,
// no HTML escaping (a "<" in SQL stays a "<" in the diff), keys in a fixed
// order, cell ids preserved, a trailing newline. Outputs are never persisted.
func EncodeFile(nb Notebook) ([]byte, error) {
	cells := nb.Cells
	if cells == nil {
		cells = []Cell{}
	}
	doc := fileDoc{SchemaVersion: SchemaVersion, Title: nb.Title, Cells: cells}
	return encodeJSONStable(doc)
}

// encodeJSONStable marshals v with 2-space indent and without HTML escaping,
// including inside values that marshal themselves (Cell, a2ui.Page) and so
// were already escaped by encoding/json before the encoder saw them.
func encodeJSONStable(v any) ([]byte, error) {
	b, err := jsonenc.Encode(v, "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode notebook: %w", err)
	}
	return unescapeHTML(b), nil
}

// unescapeHTML rewrites the <, > and & escapes encoding/json
// emits for < > & inside nested Marshalers back to the characters themselves.
// A backslash that is itself escaped (\\u003c) is left alone.
func unescapeHTML(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' {
			if i+1 < len(b) && b[i+1] == 'u' && i+5 < len(b) && b[i+2] == '0' && b[i+3] == '0' {
				switch string(b[i+2 : i+6]) {
				case "003c":
					out = append(out, '<')
					i += 5
					continue
				case "003e":
					out = append(out, '>')
					i += 5
					continue
				case "0026":
					out = append(out, '&')
					i += 5
					continue
				}
			}
			// Copy the escape pair whole so "\\" never starts a false match.
			out = append(out, b[i])
			if i+1 < len(b) {
				i++
				out = append(out, b[i])
			}
			continue
		}
		out = append(out, b[i])
	}
	return out
}
