package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"gopkg.in/yaml.v3"
)

// scalarLoc is a single-line scalar found in a YAML document, with its
// position so it can be replaced in place.
type scalarLoc struct {
	value       string
	line, col   int // 1-based, as yaml.v3 reports
	style       yaml.Style
	rawStartIdx int // byte offset of the scalar's first character
	rawEndIdx   int // byte offset one past its last character
}

// findYAMLScalar locates the scalar at the key path (mapping keys only).
func findYAMLScalar(data []byte, keys ...string) (scalarLoc, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return scalarLoc{}, fmt.Errorf("config file is not valid YAML: %w", err)
	}
	n := &doc
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	for _, k := range keys {
		if n.Kind != yaml.MappingNode {
			return scalarLoc{}, errors.New("key not found")
		}
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				next = n.Content[i+1]
			}
		}
		if next == nil {
			return scalarLoc{}, errors.New("key not found")
		}
		n = next
	}
	if n.Kind != yaml.ScalarNode {
		return scalarLoc{}, errors.New("key is not a scalar")
	}
	loc := scalarLoc{value: n.Value, line: n.Line, col: n.Column, style: n.Style}
	// Byte offset of the scalar start.
	off, ok := lineOffset(data, n.Line)
	if !ok {
		return scalarLoc{}, errors.New("cannot locate the key")
	}
	loc.rawStartIdx = off + n.Column - 1
	end, err := scalarEnd(data, loc.rawStartIdx, n)
	if err != nil {
		return scalarLoc{}, err
	}
	loc.rawEndIdx = end
	return loc, nil
}

func lineOffset(data []byte, line int) (int, bool) {
	off := 0
	for i := 1; i < line; i++ {
		j := bytes.IndexByte(data[off:], '\n')
		if j < 0 {
			return 0, false
		}
		off += j + 1
	}
	return off, true
}

// scalarEnd finds where the scalar's text ends on its line.
func scalarEnd(data []byte, start int, n *yaml.Node) (int, error) {
	if start >= len(data) {
		return 0, errors.New("cannot locate the key")
	}
	eol := bytes.IndexByte(data[start:], '\n')
	lineEnd := len(data)
	if eol >= 0 {
		lineEnd = start + eol
	}
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		for i := start + 1; i < lineEnd; i++ {
			switch data[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, errors.New("the key spans several lines; remove it from the file by hand")
	case n.Style&yaml.SingleQuotedStyle != 0:
		for i := start + 1; i < lineEnd; i++ {
			if data[i] == '\'' {
				if i+1 < lineEnd && data[i+1] == '\'' {
					i++
					continue
				}
				return i + 1, nil
			}
		}
		return 0, errors.New("the key spans several lines; remove it from the file by hand")
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return 0, errors.New("the key is a block scalar; remove it from the file by hand")
	}
	// Plain scalar: its text is n.Value, verbatim, if it fits on the line.
	if bytes.HasPrefix(data[start:lineEnd], []byte(n.Value)) {
		return start + len(n.Value), nil
	}
	return 0, errors.New("the key spans several lines; remove it from the file by hand")
}

// blank returns data with the scalar replaced by "".
func (l scalarLoc) blank(data []byte) ([]byte, error) {
	if l.rawEndIdx <= l.rawStartIdx || l.rawEndIdx > len(data) {
		return nil, errors.New("cannot locate the key")
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:l.rawStartIdx]...)
	out = append(out, '"', '"')
	out = append(out, data[l.rawEndIdx:]...)
	return out, nil
}

// verifyBlanked checks the edited document parses, that the only difference
// is llm.api_key becoming empty, and that the old value is gone.
func verifyBlanked(orig, edited []byte) error {
	var a, b map[string]any
	if err := yaml.Unmarshal(orig, &a); err != nil {
		return fmt.Errorf("config file is not valid YAML: %w", err)
	}
	if err := yaml.Unmarshal(edited, &b); err != nil {
		return errors.New("the edit would leave an invalid config file; it was not changed")
	}
	llm, ok := a["llm"].(map[string]any)
	if !ok {
		return errors.New("the config file has no llm section")
	}
	old, _ := llm["api_key"].(string)
	llm2 := map[string]any{}
	for k, v := range llm {
		llm2[k] = v
	}
	llm2["api_key"] = ""
	a["llm"] = llm2
	if !reflect.DeepEqual(a, b) {
		return errors.New("the edit would change more than the API key; the config file was not changed")
	}
	if old != "" && strings.Contains(string(edited), old) {
		return errors.New("the key still appears in the config file (is it repeated in a comment?); remove it by hand")
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	return filestore.WriteFileAtomicExact(path, data, mode)
}

// yamlDocCount counts the documents in data.
func yamlDocCount(data []byte) (int, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	n := 0
	for {
		var v yaml.Node
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		n++
	}
}
