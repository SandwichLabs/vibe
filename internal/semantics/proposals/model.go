// Package proposals is the inbox of proposed semantic definitions: an agent
// (or, later, a verifier) suggests an entity, metric, join or glossary term,
// and a person accepts or rejects it. A proposal is a file in
// .slaw-local/proposals/ (machine-local, git-ignored, never in a bundle) that
// holds the exact definition that would be written, so accepting it can never
// produce a file the semantics decoder would refuse: the definition is
// validated with the kind's own decoder when it is proposed, again whenever
// the proposal is read, and once more when it is accepted.
//
// Two interfaces keep the roles apart. Proposer (create, list, count) is all an
// agent surface is given. Store adds Get and Decide, and Accept and Reject
// (which also need a semantics.Store) are functions only a person's surface
// calls: the CLI's `slaw semantics accept` and Studio's bindings. Nothing in
// internal/agents can reach them. Reference: docs/reference/semantics.md.
package proposals

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/connname"
	"github.com/SandwichLabs/slaw/internal/filestore"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/strictyaml"
)

// Dir is the proposals folder under the workspace root. It sits in the
// git-ignored .slaw-local/ and no bundle ever reads it.
const Dir = ".slaw-local/proposals"

// Extension of a proposal file.
const Extension = ".yaml"

// Version is the only document version there is.
const Version = 1

// Statuses.
const (
	StatusOpen     = "open"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
)

// Who proposed.
const (
	ByAgent  = "agent"
	ByPerson = "person"
)

// Caps. A proposal is one definition plus a few lines of reasoning.
const (
	MaxBytes       = 64 << 10
	MaxNodes       = 6000
	MaxOpen        = 200
	MaxFiles       = 1000
	PruneTo        = 800
	MaxRationale   = 2000
	MaxEvidence    = 20
	MaxEvidenceLen = 300
	maxRevLen      = 128
)

// Errors a caller can test with errors.Is.
var (
	// ErrInvalid wraps every refusal of a proposal's content.
	ErrInvalid = semantics.ErrInvalid
	// ErrNotFound means there is no such proposal.
	ErrNotFound = filestore.ErrNotFound
	// ErrConflict means the proposal file changed since it was read, or the
	// definition it would replace did (see StaleError).
	ErrConflict = filestore.ErrConflict
	// ErrNotOpen means the proposal was already accepted or rejected.
	ErrNotOpen = errors.New("proposal is not open")
	// ErrTooMany means MaxOpen proposals are waiting.
	ErrTooMany = errors.New("too many open proposals: accept or reject some first")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Kinds that can be proposed.
var Kinds = []semantics.Kind{semantics.KindEntity, semantics.KindMetric, semantics.KindJoin, semantics.KindGlossary}

// ValidKind reports whether k can be proposed.
func ValidKind(k semantics.Kind) bool {
	for _, v := range Kinds {
		if v == k {
			return true
		}
	}
	return false
}

// idRE is the shape of a proposal id: <UTC time>-<kind>-<name>-<4 hex>. It is
// also the only shape a store turns into a file name, so a caller-supplied id
// can never name a path.
var idRE = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-(entity|metric|join|glossary)-[a-z_][a-z0-9_]{0,62}-[0-9a-f]{4}$`)

// ValidID reports whether id has the shape Slaw generates.
func ValidID(id string) bool { return idRE.MatchString(id) }

// Proposal is one suggested definition. Definition is the exact document to
// write (for a glossary, {term, text}); the rest says who proposed it, why and
// what it replaces.
type Proposal struct {
	Version    int            `yaml:"version" json:"version"`
	ID         string         `yaml:"id" json:"id"`
	Kind       semantics.Kind `yaml:"kind" json:"kind"`
	Name       string         `yaml:"name" json:"name"`
	Status     string         `yaml:"status" json:"status"`
	ProposedAt string         `yaml:"proposed_at" json:"proposed_at"`
	ProposedBy string         `yaml:"proposed_by" json:"proposed_by"`
	Rationale  string         `yaml:"rationale" json:"rationale"`
	Evidence   []string       `yaml:"evidence" json:"evidence"`
	// ReplacesRev is the revision of the file this replaces, as it was when
	// the proposal was made ("" = it did not exist).
	ReplacesRev string    `yaml:"replaces_rev" json:"replaces_rev"`
	Definition  yaml.Node `yaml:"definition" json:"-"`
	DecidedAt   string    `yaml:"decided_at" json:"decided_at"`

	semantics.Meta `yaml:"-"`
}

// Open reports whether the proposal still waits for a decision.
func (p *Proposal) Open() bool { return p.Error == "" && p.Status == StatusOpen }

// Target is the workspace-relative file accepting the proposal writes.
func (p *Proposal) Target() string {
	switch p.Kind {
	case semantics.KindEntity:
		return semantics.Dir + "/" + semantics.EntitiesDir + "/" + p.Name + semantics.Extension
	case semantics.KindMetric:
		return semantics.Dir + "/" + semantics.MetricsDir + "/" + p.Name + semantics.Extension
	case semantics.KindJoin:
		return semantics.Dir + "/" + semantics.JoinsDir + "/" + p.Name + semantics.Extension
	}
	return semantics.Dir + "/" + semantics.GlossaryFile
}

// glossaryDef is a glossary proposal's definition.
type glossaryDef struct {
	Term string `yaml:"term" json:"term"`
	Text string `yaml:"text" json:"text"`
}

// GlossaryTerm returns a glossary proposal's term and text.
func (p *Proposal) GlossaryTerm() (term, text string, err error) {
	if p.Kind != semantics.KindGlossary {
		return "", "", invalid("a %s proposal has no glossary term", p.Kind)
	}
	data, err := yaml.Marshal(&p.Definition)
	if err != nil {
		return "", "", fmt.Errorf("failed to read the proposal's definition: %w", err)
	}
	return decodeGlossaryDef(data)
}

func decodeGlossaryDef(data []byte) (term, text string, err error) {
	var g glossaryDef
	if err := strictyaml.Decode(data, semantics.MaxTermText+4096, MaxNodes, "definition", &g); err != nil {
		return "", "", invalid("%s", err.Error())
	}
	if err := semantics.ValidateGlossaryTerm(g.Term, g.Text); err != nil {
		return "", "", err
	}
	return g.Term, g.Text, nil
}

// Render returns the text accepting the proposal would write, decoded and
// re-encoded with the kind's own rules: the file for an entity, metric or
// join, the "## term" section for a glossary. It is what a person is shown and
// what Accept writes.
func (p *Proposal) Render() (string, error) {
	if p.Kind == semantics.KindGlossary {
		term, text, err := p.GlossaryTerm()
		if err != nil {
			return "", err
		}
		return "## " + term + "\n\n" + strings.TrimSpace(text) + "\n", nil
	}
	data, err := yaml.Marshal(&p.Definition)
	if err != nil {
		return "", fmt.Errorf("failed to read the proposal's definition: %w", err)
	}
	return renderDefinition(p.Kind, p.Name, data)
}

// renderDefinition decodes data as a kind's document called name and encodes
// it canonically.
func renderDefinition(kind semantics.Kind, name string, data []byte) (string, error) {
	var out []byte
	switch kind {
	case semantics.KindEntity:
		e, err := semantics.DecodeEntity(name, data)
		if err != nil {
			return "", err
		}
		out, err = semantics.EncodeEntity(e)
		if err != nil {
			return "", err
		}
	case semantics.KindMetric:
		m, err := semantics.DecodeMetric(name, data)
		if err != nil {
			return "", err
		}
		out, err = semantics.EncodeMetric(m)
		if err != nil {
			return "", err
		}
	case semantics.KindJoin:
		j, err := semantics.DecodeJoin(name, data)
		if err != nil {
			return "", err
		}
		if j.Measured != nil {
			return "", invalid("a join proposal may not carry measured: it is written by a profiling run the person starts")
		}
		out, err = semantics.EncodeJoin(j)
		if err != nil {
			return "", err
		}
	default:
		return "", invalid("unknown kind %q", kind)
	}
	return string(out), nil
}

// Validate checks a proposal's structure, including that its definition passes
// the kind's own decoder. stem, when set, must equal the id.
func Validate(p *Proposal, stem string) error {
	if p.Version != Version {
		return invalid("version must be %d (got %d)", Version, p.Version)
	}
	if !ValidID(p.ID) {
		return invalid("id %q is not a proposal id", p.ID)
	}
	if stem != "" && p.ID != stem {
		return invalid("id %q does not match the file name %q", p.ID, stem)
	}
	if !ValidKind(p.Kind) {
		return invalid("kind %q is not one of entity, metric, join, glossary", p.Kind)
	}
	if !connname.Valid(p.Name) || !strings.Contains(p.ID, "-"+string(p.Kind)+"-"+p.Name+"-") {
		return invalid("name %q does not match the id", p.Name)
	}
	if err := checkStatus(p); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, p.ProposedAt); err != nil {
		return invalid("proposed_at %q is not an RFC 3339 time", p.ProposedAt)
	}
	if p.ProposedBy != ByAgent && p.ProposedBy != ByPerson {
		return invalid("proposed_by %q is not agent or person", p.ProposedBy)
	}
	if err := checkText("rationale", p.Rationale, MaxRationale, true); err != nil {
		return err
	}
	if err := checkEvidence(p.Evidence); err != nil {
		return err
	}
	if len(p.ReplacesRev) > maxRevLen {
		return invalid("replaces_rev is over %d characters", maxRevLen)
	}
	if err := checkText("replaces_rev", p.ReplacesRev, maxRevLen, false); err != nil {
		return err
	}
	if p.Definition.Kind != yaml.MappingNode {
		return invalid("definition must be a mapping")
	}
	text, err := p.Render()
	if err != nil {
		return err
	}
	if p.Kind == semantics.KindGlossary && p.Name != GlossarySlug(firstLine(text)[3:]) {
		return invalid("name %q does not match the glossary term", p.Name)
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func checkStatus(p *Proposal) error {
	switch p.Status {
	case StatusOpen:
		if p.DecidedAt != "" {
			return invalid("an open proposal has no decided_at")
		}
	case StatusAccepted, StatusRejected:
		if _, err := time.Parse(time.RFC3339, p.DecidedAt); err != nil {
			return invalid("decided_at %q is not an RFC 3339 time", p.DecidedAt)
		}
	default:
		return invalid("status %q is not open, accepted or rejected", p.Status)
	}
	return nil
}

func checkText(field, v string, max int, required bool) error {
	if required && strings.TrimSpace(v) == "" {
		return invalid("%s is required", field)
	}
	if n := utf8.RuneCountInString(v); n > max {
		return invalid("%s is %d characters, over the %d character limit", field, n, max)
	}
	if r, ok := strictyaml.FirstHiddenRune(v, true); ok {
		return invalid("%s contains a control or invisible character (U+%04X)", field, r)
	}
	return nil
}

func checkEvidence(ev []string) error {
	if len(ev) > MaxEvidence {
		return invalid("%d evidence entries, over the limit of %d", len(ev), MaxEvidence)
	}
	for _, e := range ev {
		if err := checkText("an evidence entry", e, MaxEvidenceLen, true); err != nil {
			return err
		}
	}
	return nil
}

// GlossarySlug turns a glossary term into the name a proposal of it carries: a
// lower-case identifier of letters, digits and underscores.
func GlossarySlug(term string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(term) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if s := b.String(); s != "" && !strings.HasSuffix(s, "_") {
				b.WriteByte('_')
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "term"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "t_" + s
	}
	return s
}

// Decode parses and validates a proposal file. stem is the file name without
// its extension; the id inside must equal it.
func Decode(stem string, data []byte) (*Proposal, error) {
	label := Dir + "/" + stem + Extension
	var p Proposal
	if err := strictyaml.Decode(data, MaxBytes, MaxNodes, label, &p); err != nil {
		return nil, invalid("%s", err.Error())
	}
	if err := Validate(&p, stem); err != nil {
		return nil, invalid("%s: %s", label, strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": "))
	}
	if p.Evidence == nil {
		p.Evidence = []string{}
	}
	return &p, nil
}

// Encode validates and renders a proposal as file bytes.
func Encode(p *Proposal) ([]byte, error) {
	if err := Validate(p, ""); err != nil {
		return nil, err
	}
	c := *p
	if c.Evidence == nil {
		c.Evidence = []string{}
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&c); err != nil {
		return nil, fmt.Errorf("failed to encode proposal %s: %w", p.ID, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to encode proposal %s: %w", p.ID, err)
	}
	if buf.Len() > MaxBytes {
		return nil, invalid("the proposal would be %d bytes, over the %d byte limit", buf.Len(), MaxBytes)
	}
	return []byte(buf.String()), nil
}
