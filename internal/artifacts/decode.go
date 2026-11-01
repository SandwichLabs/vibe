package artifacts

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/strictyaml"
)

// Decode parses and validates artifact.yaml. It is strict: the byte cap is
// checked before anything is parsed; anchors, aliases and merge keys are
// refused (they exist only to multiply a small document into a large one, and
// a manifest has no use for them); a second YAML document, an unknown key and
// a duplicate key are errors. Nothing is interpolated: no ${ENV} anywhere.
func Decode(data []byte) (*Manifest, error) {
	var m Manifest
	if err := strictyaml.Decode(data, MaxManifestBytes, maxYAMLNodes, ManifestFile, &m); err != nil {
		return nil, invalid("%s", err.Error())
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
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
