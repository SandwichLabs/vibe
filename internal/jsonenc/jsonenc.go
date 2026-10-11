// Package jsonenc is the one JSON encoder configuration for the files and
// strings Vibe writes: HTML escaping off (Encode), so < > & stay readable and diffs
// stay small. It is a leaf package so any package may use it.
package jsonenc

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// Quote renders v as a compact JSON literal (HTML characters escaped, as
// encoding/json does by default), for messages and prompts that must show a
// value as clearly delimited data. A value that cannot be marshaled falls back
// to its %v form.
func Quote(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
