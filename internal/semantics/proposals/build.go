package proposals

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/semantics"
)

// Draft is what a proposer supplies: which definition, the document it wants
// written, and why. The id, time and status are Slaw's, never the proposer's.
type Draft struct {
	Kind semantics.Kind
	// Name is the definition's name; ignored for a glossary term, whose name
	// is made from the term.
	Name string
	// Definition is the document as a JSON-shaped value (a map from a tool
	// call). version and name default to the current version and Name. For a
	// glossary it is {term, text}.
	Definition any
	Rationale  string
	Evidence   []string
	// ReplacesRev is the revision of the existing file this replaces, as the
	// proposer read it ("" when proposing a new definition).
	ReplacesRev string
}

// Build validates a draft and returns the open proposal it makes, with a fresh
// id. The definition goes through the kind's own strict decoder (so the
// measure rule, the reserved-word rule and every cap apply) and is stored in
// its canonical encoding, which is exactly what accepting it will write.
func Build(d Draft, now time.Time, by string) (*Proposal, error) {
	if !ValidKind(d.Kind) {
		return nil, invalid("kind %q is not one of entity, metric, join, glossary", d.Kind)
	}
	node, name, err := canonicalDefinition(d)
	if err != nil {
		return nil, err
	}
	p := &Proposal{
		Version:     Version,
		Kind:        d.Kind,
		Name:        name,
		Status:      StatusOpen,
		ProposedAt:  now.UTC().Format(time.RFC3339),
		ProposedBy:  by,
		Rationale:   strings.TrimSpace(d.Rationale),
		Evidence:    append([]string{}, d.Evidence...),
		ReplacesRev: d.ReplacesRev,
		Definition:  *node,
	}
	if p.ID, err = NewID(now, d.Kind, name); err != nil {
		return nil, err
	}
	if err := Validate(p, ""); err != nil {
		return nil, err
	}
	if _, err := Encode(p); err != nil {
		return nil, err
	}
	return p, nil
}

// NewID makes a proposal id: the UTC time to the second, the kind, the name and
// four random hex digits.
func NewID(now time.Time, kind semantics.Kind, name string) (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to make a proposal id: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + string(kind) + "-" + name + "-" + hex.EncodeToString(b[:]), nil
}

// canonicalDefinition decodes the draft's document with the kind's strict
// decoder and returns its canonical encoding as a YAML mapping node, plus the
// name the proposal carries.
func canonicalDefinition(d Draft) (*yaml.Node, string, error) {
	if d.Kind == semantics.KindGlossary {
		return glossaryNode(d.Definition)
	}
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return nil, "", invalid("name is required")
	}
	doc, ok := d.Definition.(map[string]any)
	if !ok {
		return nil, "", invalid("definition must be an object")
	}
	doc = withDefaults(doc, name)
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, "", invalid("definition cannot be encoded: %v", err)
	}
	// JSON is YAML, so the strict decoder reads it as it would a file.
	text, err := renderDefinition(d.Kind, name, raw)
	if err != nil {
		return nil, "", err
	}
	var parsed yaml.Node
	if err := yaml.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, "", fmt.Errorf("failed to read back the encoded definition: %w", err)
	}
	if len(parsed.Content) != 1 {
		return nil, "", errors.New("failed to read back the encoded definition: not one document")
	}
	return parsed.Content[0], name, nil
}

func withDefaults(doc map[string]any, name string) map[string]any {
	out := make(map[string]any, len(doc)+2)
	for k, v := range doc {
		out[k] = v
	}
	if _, ok := out["version"]; !ok {
		out["version"] = semantics.Version
	}
	if _, ok := out["name"]; !ok {
		out["name"] = name
	}
	return out
}

func glossaryNode(def any) (*yaml.Node, string, error) {
	doc, ok := def.(map[string]any)
	if !ok {
		return nil, "", invalid("definition must be an object {term, text}")
	}
	for k := range doc {
		if k != "term" && k != "text" {
			return nil, "", invalid("a glossary definition has only term and text (got %q)", k)
		}
	}
	term, _ := doc["term"].(string)
	text, _ := doc["text"].(string)
	if err := semantics.ValidateGlossaryTerm(term, text); err != nil {
		return nil, "", err
	}
	text = strings.TrimSpace(text)
	var n yaml.Node
	if err := n.Encode(glossaryDef{Term: term, Text: text}); err != nil {
		return nil, "", fmt.Errorf("failed to encode the glossary definition: %w", err)
	}
	return &n, GlossarySlug(term), nil
}
