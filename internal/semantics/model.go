// Package semantics is the workspace's semantic layer as reviewed files: what
// the data means, kept beside the queries and notebooks that use it.
//
//	semantics/entities/<name>.yaml   a thing the data is about (grain, table, id, PII flags)
//	semantics/metrics/<name>.yaml    a named measure over a saved query
//	semantics/joins/<name>.yaml      how two entities relate (declared and measured cardinality)
//	semantics/glossary.md            plain markdown, one "## term" section per term
//
// The package owns the models, the strict decoder and validator, and the Store
// (a FileStore over the folder and an in-memory Mock). It decodes with
// internal/strictyaml, so a file from a cloned repo or a bundle is held to the
// same rules as an artifact.yaml: byte cap before parse, no anchors, aliases,
// tags or merge keys, unknown keys refused. It never executes anything: a
// metric's SQL is vetted where it runs, not here. Reference: docs/reference/semantics.md.
package semantics

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/assay/internal/filestore"
)

// Folder layout.
const (
	// Dir is the workspace folder; the three subfolders hold one yaml file per
	// definition, named <name>.yaml.
	Dir         = "semantics"
	EntitiesDir = "entities"
	MetricsDir  = "metrics"
	JoinsDir    = "joins"
	Extension   = ".yaml"
	// GlossaryFile is the one markdown file directly in Dir.
	GlossaryFile = "glossary.md"
	// Version is the only document version there is.
	Version = 1
)

// Caps. A definition is a small document; these are generous for a person and
// small enough that a hostile folder cannot cost memory or a long SQL line.
const (
	MaxYAMLBytes     = 64 << 10
	MaxYAMLNodes     = 5000
	MaxGlossaryBytes = 256 << 10
	MaxFilesPerKind  = 500
	MaxDescription   = 2000
	MaxTitle         = 200
	MaxGrain         = 200
	MaxOwner         = 200
	MaxMeasureBytes  = 4 << 10
	MaxDimensions    = 20
	MaxColumns       = 100
	MaxAssertions    = 50
	MaxValues        = 200
	MaxValueLen      = 200
	MaxJoinColumns   = 8
	MaxSettledText   = MaxDescription
	MaxRef           = 200
)

// Kind is which definition a name belongs to.
type Kind string

// The kinds. Glossary has no name: there is one glossary.
const (
	KindEntity   Kind = "entity"
	KindMetric   Kind = "metric"
	KindJoin     Kind = "join"
	KindGlossary Kind = "glossary"
)

// Assertion kinds. Entities take unique, not_null, accepted_values, range,
// freshness and row_count; metrics take non_negative, not_null, range and
// unique (the saved query has one row per full dimension set).
const (
	AssertUnique         = "unique"
	AssertNotNull        = "not_null"
	AssertAcceptedValues = "accepted_values"
	AssertNonNegative    = "non_negative"
	AssertRange          = "range"
	AssertFreshness      = "freshness"
	AssertRowCount       = "row_count"
)

// Join cardinalities.
const (
	OneToOne   = "one_to_one"
	ManyToOne  = "many_to_one"
	OneToMany  = "one_to_many"
	ManyToMany = "many_to_many"
)

// Metric display formats (display only).
const (
	FormatNumber   = "number"
	FormatCurrency = "currency"
	FormatPercent  = "percent"
)

// Verdicts of a join's measured profile.
const (
	VerdictConsistent = "consistent"
	VerdictFanOut     = "fan_out"
	VerdictOrphans    = "orphans"
)

// Errors a caller can test with errors.Is.
var (
	// ErrInvalid wraps every structural refusal: a document that must not be
	// stored or used, so a caller can tell it from I/O and conflicts.
	ErrInvalid = errors.New("invalid semantics definition")
	// ErrConflict means the file changed on disk since it was loaded.
	ErrConflict = filestore.ErrConflict
	// ErrNotFound means there is no such definition.
	ErrNotFound = filestore.ErrNotFound
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// notFoundError is what Get and Delete return for an unknown name; it
// satisfies errors.Is(err, ErrNotFound).
type notFoundError struct {
	kind Kind
	name string
}

func (e *notFoundError) Error() string        { return fmt.Sprintf("%s %q not found", e.kind, e.name) }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// Meta is what a store adds to a definition it loaded; it is never part of the
// file.
type Meta struct {
	// Rev is the content hash of the one file as loaded; hand it back on a
	// write and a stale write is ErrConflict.
	Rev string `yaml:"-" json:"rev,omitempty"`
	// Error is set for a file that could not be read (bad yaml, too large, a
	// name that does not match the file): it still lists, under its file
	// stem, so it can be found and fixed, and it is never saved over without
	// Overwrite.
	Error string `yaml:"-" json:"error,omitempty"`
}

// Entity is a thing the data is about.
type Entity struct {
	Version     int         `yaml:"version" json:"version"`
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Grain       string      `yaml:"grain" json:"grain"`
	Table       string      `yaml:"table" json:"table"`
	Connection  string      `yaml:"connection,omitempty" json:"connection,omitempty"`
	IDColumn    string      `yaml:"id_column" json:"id_column"`
	Columns     []Column    `yaml:"columns,omitempty" json:"columns,omitempty"`
	Owner       string      `yaml:"owner,omitempty" json:"owner,omitempty"`
	Assertions  []Assertion `yaml:"assertions,omitempty" json:"assertions,omitempty"`

	Meta `yaml:"-"`
}

// Column annotates one column of an entity's table.
type Column struct {
	Name        string `yaml:"name" json:"name"`
	PII         bool   `yaml:"pii,omitempty" json:"pii,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Assertion is a check a person declares; the assert package plans and runs
// it. Column and Values apply to entity assertions only; Min and Max belong to
// range, MaxAge to freshness and MinRows to row_count.
type Assertion struct {
	Kind    string   `yaml:"kind" json:"kind"`
	Column  string   `yaml:"column,omitempty" json:"column,omitempty"`
	Values  []string `yaml:"values,omitempty" json:"values,omitempty"`
	Min     *Number  `yaml:"min,omitempty" json:"min,omitempty"`
	Max     *Number  `yaml:"max,omitempty" json:"max,omitempty"`
	MaxAge  string   `yaml:"max_age,omitempty" json:"max_age,omitempty"`
	MinRows *Count   `yaml:"min_rows,omitempty" json:"min_rows,omitempty"`
}

// Metric is a named measure over a saved query.
type Metric struct {
	Version     int         `yaml:"version" json:"version"`
	Name        string      `yaml:"name" json:"name"`
	Title       string      `yaml:"title,omitempty" json:"title,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Query       string      `yaml:"query" json:"query"`
	Measure     string      `yaml:"measure" json:"measure"`
	Dimensions  []string    `yaml:"dimensions,omitempty" json:"dimensions,omitempty"`
	Format      string      `yaml:"format,omitempty" json:"format,omitempty"`
	Owner       string      `yaml:"owner,omitempty" json:"owner,omitempty"`
	SettledBy   *SettledBy  `yaml:"settled_by,omitempty" json:"settled_by,omitempty"`
	Assertions  []Assertion `yaml:"assertions,omitempty" json:"assertions,omitempty"`

	Meta `yaml:"-"`
}

// SettledBy records the disagreement that settled a definition. Ref is stored
// text only (it will name a verifier card or receipt); nothing resolves it.
type SettledBy struct {
	Text string `yaml:"text" json:"text"`
	Ref  string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// Join says how two entities relate.
type Join struct {
	Version     int       `yaml:"version" json:"version"`
	Name        string    `yaml:"name" json:"name"`
	Left        string    `yaml:"left" json:"left"`
	Right       string    `yaml:"right" json:"right"`
	On          []JoinOn  `yaml:"on" json:"on"`
	Cardinality string    `yaml:"cardinality" json:"cardinality"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Measured    *Measured `yaml:"measured,omitempty" json:"measured,omitempty"`

	Meta `yaml:"-"`
}

// JoinOn pairs a column of the left entity with one of the right.
type JoinOn struct {
	Left  string `yaml:"left" json:"left"`
	Right string `yaml:"right" json:"right"`
}

// Measured is the result of a profiling run the person started.
type Measured struct {
	At           string `yaml:"at" json:"at"`
	LeftRows     Count  `yaml:"left_rows" json:"left_rows"`
	RightRows    Count  `yaml:"right_rows" json:"right_rows"`
	MaxFanout    Count  `yaml:"max_fanout" json:"max_fanout"`
	LeftOrphans  Count  `yaml:"left_orphans" json:"left_orphans"`
	RightOrphans Count  `yaml:"right_orphans" json:"right_orphans"`
	Verdict      string `yaml:"verdict" json:"verdict"`
}

// Count is a row count in a join's measured block. It is read strictly: a
// non-negative whole number written in decimal. YAML would otherwise take 1.5
// as 1 and 0x10 as 16.
type Count int64

var countRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Count) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || !countRE.MatchString(n.Value) {
		return fmt.Errorf("line %d: a count must be a non-negative whole number written in decimal (got %q)", n.Line, n.Value)
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*c = Count(v)
	return nil
}

// Glossary is semantics/glossary.md: markdown as written, plus the terms its
// "## " headings define. A missing file is an empty Glossary with no Rev.
type Glossary struct {
	Text  string   `json:"text"`
	Terms []string `json:"terms"`

	Meta
}

// Number is a bound of a range assertion. It is read strictly: a finite
// decimal number written plainly (-5, 0, 100, 0.25). A quoted "100", 1e3, 0x10,
// .inf and .nan are refused rather than coerced, so what the file says is what
// is compared.
type Number float64

var numberRE = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,14})(\.[0-9]{1,15})?$`)

// UnmarshalYAML implements yaml.Unmarshaler.
func (v *Number) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!int" && n.Tag != "!!float") || !numberRE.MatchString(n.Value) {
		return fmt.Errorf("line %d: a bound must be a plain decimal number such as 0, -5 or 99.5, not quoted and without an exponent (got %q)", n.Line, n.Value)
	}
	f, err := strconv.ParseFloat(n.Value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("line %d: %q is not a finite number", n.Line, n.Value)
	}
	*v = Number(f)
	return nil
}

// MarshalYAML writes the number back in the plain decimal form UnmarshalYAML
// reads (never an exponent).
func (v Number) MarshalYAML() (any, error) {
	text := strconv.FormatFloat(float64(v), 'f', -1, 64)
	tag := "!!int"
	if strings.Contains(text, ".") {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: text}, nil
}

// Float is the bound as a float64, for a bound parameter.
func (v Number) Float() float64 { return float64(v) }
