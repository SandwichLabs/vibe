package artifacts

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Decode parses and validates artifact.yaml. It is strict: the byte cap is
// checked before anything is parsed; anchors, aliases and merge keys are
// refused (they exist only to multiply a small document into a large one, and
// a manifest has no use for them); a second YAML document, an unknown key and
// a duplicate key are errors. Nothing is interpolated: no ${ENV} anywhere.
func Decode(data []byte) (*Manifest, error) {
	if len(data) > MaxManifestBytes {
		return nil, invalid("%s is %d bytes, over the %d byte limit", ManifestFile, len(data), MaxManifestBytes)
	}
	if r, ok := firstHiddenRune(string(data), false); ok {
		return nil, invalid("%s contains a control character (U+%04X)", ManifestFile, r)
	}
	if err := checkYAMLShape(data); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, invalid("%s is empty", ManifestFile)
		}
		return nil, invalid("%s: %v", ManifestFile, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, invalid("%s must hold a single YAML document", ManifestFile)
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// checkYAMLShape parses into the node tree (which never expands aliases) and
// refuses anchors, aliases, merge keys and an oversized tree.
func checkYAMLShape(data []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return invalid("%s: %v", ManifestFile, err)
	}
	count := 0
	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		count++
		if count > maxYAMLNodes {
			return invalid("%s has too many YAML nodes (limit %d)", ManifestFile, maxYAMLNodes)
		}
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return invalid("%s may not use YAML anchors or aliases", ManifestFile)
		}
		if n.Style&yaml.TaggedStyle != 0 {
			return invalid("%s may not use explicit YAML tags", ManifestFile)
		}
		if n.Kind == yaml.ScalarNode && n.Tag == "!!merge" {
			return invalid("%s may not use YAML merge keys", ManifestFile)
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

// Encode renders a manifest as artifact.yaml bytes after validating it.
func Encode(m *Manifest) ([]byte, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", ManifestFile, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", ManifestFile, err)
	}
	if buf.Len() > MaxManifestBytes {
		return nil, invalid("%s would be %d bytes, over the %d byte limit", ManifestFile, buf.Len(), MaxManifestBytes)
	}
	return buf.Bytes(), nil
}
