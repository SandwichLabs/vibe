package semantics

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/vibe/internal/strictyaml"
)

// decodeDoc is the one strict path for the three yaml kinds.
func decodeDoc(label string, data []byte, into any) error {
	if err := strictyaml.Decode(data, MaxYAMLBytes, MaxYAMLNodes, label, into); err != nil {
		return invalid("%s", err.Error())
	}
	return nil
}

// relabel puts the file in front of a validator's message, keeping ErrInvalid
// as the one prefix.
func relabel(label string, err error) error {
	return invalid("%s: %s", label, errorText(err))
}

func checkStem(label, name, stem string) error {
	if stem != "" && name != stem {
		return invalid("%s: name %q does not match the file name %q", label, name, stem)
	}
	return nil
}

func fileLabel(dir, stem string) string { return Dir + "/" + dir + "/" + stem + Extension }

// DecodeEntity parses and validates an entity file. stem is the file name
// without its extension; the name inside must equal it ("" skips that check).
func DecodeEntity(stem string, data []byte) (*Entity, error) {
	label := fileLabel(EntitiesDir, stem)
	var e Entity
	if err := decodeDoc(label, data, &e); err != nil {
		return nil, err
	}
	if err := ValidateEntity(&e); err != nil {
		return nil, relabel(label, err)
	}
	if err := checkStem(label, e.Name, stem); err != nil {
		return nil, err
	}
	return &e, nil
}

// DecodeMetric parses and validates a metric file (see DecodeEntity).
func DecodeMetric(stem string, data []byte) (*Metric, error) {
	label := fileLabel(MetricsDir, stem)
	var m Metric
	if err := decodeDoc(label, data, &m); err != nil {
		return nil, err
	}
	if err := ValidateMetric(&m); err != nil {
		return nil, relabel(label, err)
	}
	if err := checkStem(label, m.Name, stem); err != nil {
		return nil, err
	}
	return &m, nil
}

// DecodeJoin parses and validates a join file (see DecodeEntity).
func DecodeJoin(stem string, data []byte) (*Join, error) {
	label := fileLabel(JoinsDir, stem)
	var j Join
	if err := decodeDoc(label, data, &j); err != nil {
		return nil, err
	}
	if err := ValidateJoin(&j); err != nil {
		return nil, relabel(label, err)
	}
	if err := checkStem(label, j.Name, stem); err != nil {
		return nil, err
	}
	return &j, nil
}

// encodeDoc renders a validated document as file bytes.
func encodeDoc(label string, v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", label, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", label, err)
	}
	if buf.Len() > MaxYAMLBytes {
		return nil, invalid("%s would be %d bytes, over the %d byte limit", label, buf.Len(), MaxYAMLBytes)
	}
	return buf.Bytes(), nil
}

// EncodeEntity validates and renders an entity as file bytes. A zero Version
// is written as the current one.
func EncodeEntity(e *Entity) ([]byte, error) {
	c := *e
	if c.Version == 0 {
		c.Version = Version
	}
	if err := ValidateEntity(&c); err != nil {
		return nil, err
	}
	return encodeDoc(fileLabel(EntitiesDir, c.Name), &c)
}

// EncodeMetric validates and renders a metric as file bytes.
func EncodeMetric(m *Metric) ([]byte, error) {
	c := *m
	if c.Version == 0 {
		c.Version = Version
	}
	if err := ValidateMetric(&c); err != nil {
		return nil, err
	}
	return encodeDoc(fileLabel(MetricsDir, c.Name), &c)
}

// EncodeJoin validates and renders a join as file bytes.
func EncodeJoin(j *Join) ([]byte, error) {
	c := *j
	if c.Version == 0 {
		c.Version = Version
	}
	if err := ValidateJoin(&c); err != nil {
		return nil, err
	}
	return encodeDoc(fileLabel(JoinsDir, c.Name), &c)
}

// glossaryHeadingRE matches a level-2 ATX heading line.
var glossaryHeadingRE = regexp.MustCompile(`^## +(\S.*?)(?: +#+)?\s*$`)

// DecodeGlossary checks glossary.md (size cap, no hidden control characters)
// and lists its terms: the "## " headings outside fenced code blocks, in file
// order. The text is kept exactly as written.
func DecodeGlossary(data []byte) (*Glossary, error) {
	label := Dir + "/" + GlossaryFile
	if len(data) > MaxGlossaryBytes {
		return nil, invalid("%s is %d bytes, over the %d byte limit", label, len(data), MaxGlossaryBytes)
	}
	if r, ok := strictyaml.FirstHiddenRune(string(data), false); ok {
		return nil, invalid("%s contains a control character (U+%04X)", label, r)
	}
	text := string(data)
	g := &Glossary{Text: text, Terms: []string{}}
	fence := ""
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			switch marker := trimmed[:3]; {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := glossaryHeadingRE.FindStringSubmatch(line); m != nil {
			g.Terms = append(g.Terms, m[1])
		}
	}
	return g, nil
}
