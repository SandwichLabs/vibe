// Package artifacts is the file-backed store of data artifacts: a
// parameterised, live view stored as plain files under artifacts/<name>/. It
// holds the manifest model (artifact.yaml), its strict decoder and structural
// validator, the binder that turns (query name, raw values) into SQL plus bound
// parameters, and the store with its one two-file revision.
//
// It does structure, never SQL policy: whether a query is allowed to run is
// decided by internal/agents (it cannot be imported from here). Binding never
// puts a value into SQL text; the only output a value reaches is the params
// map.
package artifacts

import (
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Folder and file names. An artifact is artifacts/<name>/artifact.yaml plus
// artifacts/<name>/view.a2ui.json.
const (
	Dir          = "artifacts"
	ManifestFile = "artifact.yaml"
	ViewFile     = "view.a2ui.json"
)

// Caps (bytes unless noted). The byte caps apply before any decode.
const (
	MaxManifestBytes = 256 << 10
	MaxViewBytes     = 1 << 20
	MaxQueries       = 20
	MaxParams        = 50
	MaxSQLBytes      = 64 << 10
	MaxTextLength    = 1000
	MaxEnumOptions   = 200
	MaxTitleLength   = 200
	MaxDescription   = 2000
	MaxNameLength    = 64
	// maxYAMLNodes bounds the parsed document, independent of its byte size.
	maxYAMLNodes = 20000
	// MaxSafeInteger is 2^53: an integer parameter must be within it, since
	// values cross the bridge as JSON numbers.
	MaxSafeInteger = 1 << 53
)

// Manifest version and view kinds.
const (
	Version  = 1
	ViewA2UI = "a2ui"
)

// Parameter types (closed).
const (
	TypeText    = "text"
	TypeNumber  = "number"
	TypeInteger = "integer"
	TypeDate    = "date"
	TypeBoolean = "boolean"
	TypeEnum    = "enum"
)

// Manifest is artifact.yaml: typed parameters with defaults and NAMED queries
// over them. It never holds results.
type Manifest struct {
	Version     int     `yaml:"version" json:"version"`
	Title       string  `yaml:"title" json:"title"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  []Param `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Queries     []Query `yaml:"queries" json:"queries"`
	// View is the view kind: "a2ui" (the default when empty).
	View string `yaml:"view,omitempty" json:"view,omitempty"`
}

// Param declares one parameter. Field names align with queries.Param. Every
// parameter has a Default (a pointer so that default: "" is told from none).
type Param struct {
	Name  string `yaml:"name" json:"name"`
	Type  string `yaml:"type" json:"type"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
	// Default is the value used when a call omits the parameter. For a date
	// it may also be one of the closed relative forms (see ResolveDateDefault).
	Default *string `yaml:"default" json:"default"`

	MaxLength    int      `yaml:"max_length,omitempty" json:"max_length,omitempty"`
	Min          *float64 `yaml:"min,omitempty" json:"min,omitempty"`
	Max          *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	Options      []string `yaml:"options,omitempty" json:"options,omitempty"`
	OptionsQuery string   `yaml:"options_query,omitempty" json:"options_query,omitempty"`
}

// Query is one named query. SQL is stored as written; the parameters it uses
// are derived (Uses), never declared, so the two cannot disagree.
type Query struct {
	Name       string `yaml:"name" json:"name"`
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	SQL        string `yaml:"sql" json:"sql"`
	// Uses is the parameter names SQL takes, in order of first appearance.
	// Filled by Validate; ignored on input.
	Uses []string `yaml:"-" json:"uses,omitempty"`
}

// Artifact is one stored artifact: the manifest and the raw A2UI view, as a
// pair. Rev is one revision over both files.
type Artifact struct {
	Name     string
	Manifest Manifest
	// ManifestText, when set on a Put, is written as artifact.yaml as given
	// (comments and layout kept) instead of the encoding of Manifest. It must
	// pass Decode, and the Put replaces Manifest with what it decoded, so the
	// two cannot disagree. A successful Put clears it. Never set on a value a
	// store returns.
	ManifestText []byte
	// View is the view document (JSON text), validated against the manifest
	// (ValidateView): within the cap, one JSON object, a valid A2UI view.
	View []byte
	Rev  string
	// UpdatedAt is the newer of the two files' modification times (RFC 3339).
	UpdatedAt string
	// Error is set by List for a folder that could not be read as an artifact
	// (bad name, missing or invalid file, symbolic link, too large). It still
	// lists, with the folder name as Name, so it can be found and fixed. Such
	// an artifact does not run and cannot be saved over without Overwrite.
	Error string
}

// ErrConflict means the artifact changed on disk since it was loaded.
var ErrConflict = filestore.ErrConflict

// ErrNotFound means there is no such artifact.
var ErrNotFound = filestore.ErrNotFound

// ErrInvalid wraps every structural refusal (a manifest or view that must not
// be stored or run), so a caller can tell it from I/O and conflicts.
var ErrInvalid = errors.New("invalid artifact")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
