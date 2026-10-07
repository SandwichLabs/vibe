// Package jsonenc is the one JSON encoder configuration for the files and
// strings Vibe writes: HTML escaping off, so < > & stay readable and diffs
// stay small. It is a leaf package so any package may use it.
package jsonenc

import (
	"bytes"
	"encoding/json"
)

// Encode marshals v like json.Encoder.Encode with SetEscapeHTML(false): the
// output ends with a newline. A non-empty indent selects two-level pretty
// printing with that indent string and no prefix.
func Encode(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
