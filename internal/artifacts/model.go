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
// one view file: view.a2ui.json (view: a2ui) or view.js (view: script).
const (
	Dir          = "artifacts"
	ManifestFile = "artifact.yaml"
	ViewFile     = "view.a2ui.json"
	ScriptFile   = "view.js"
)

// Caps (bytes unless noted). The byte caps apply before any decode.
const (
	MaxManifestBytes = 256 << 10
	MaxViewBytes     = 1 << 20
	// MaxScriptBytes caps view.js.
	MaxScriptBytes = 1 << 20
	MaxQueries     = 20
	MaxParams      = 50
	MaxSQLBytes    = 64 << 10
	MaxTextLength  = 1000
	MaxEnumOptions = 200
	MaxTitleLength = 200
	MaxDescription = 2000
	MaxNameLength  = 64
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
	// ViewScript is a custom view: view.js, run in a Worker only after the
	// person allows it (see ViewHash and docs/reference/artifacts.md).
	ViewScript = "script"
	// ViewHTML is an HTML page: an html/template file set under html/, shown
	// only after the person allows it (see PageHash). The agent cannot write
	// one yet, and nothing renders it yet (docs/plans/html-artifacts.md).
	ViewHTML = "html"
)

// HTMLDir is the folder of a view: html artifact's page files, beside
// artifact.yaml.
const HTMLDir = "html"

// DefaultEntry is the entry template when html.entry is not set.
const DefaultEntry = "index.html.tmpl"

// Capabilities a scripted page may declare (closed set, H5).
const (
	CapState    = "state"
	CapDownload = "download"
	CapAsk      = "ask"
)

// Window bounds for a scripted page's window (pixels).
const (
	MinWindowSize = 200
	MaxWindowSize = 4000
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
	// View is the view kind: "a2ui" (the default when empty) or "script".
	View string `yaml:"view,omitempty" json:"view,omitempty"`
	// HTML configures a view: html page. It is refused with any other view.
	HTML *HTMLSpec `yaml:"html,omitempty" json:"html,omitempty"`
}

// HTMLSpec is the html: block of a view: html manifest.
type HTMLSpec struct {
	// Entry is the entry template, a .tmpl directly under html/ (default
	// DefaultEntry). Other .tmpl files are partials.
	Entry string `yaml:"entry,omitempty" json:"entry,omitempty"`
	// Window sizes a scripted page's window; a static page has none.
	Window *Window `yaml:"window,omitempty" json:"window,omitempty"`
	// Capabilities is what a scripted page declares it needs; a static page
	// declares none ("capabilities need script").
	Capabilities map[string]Capability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// Window is the requested size of a page window.
type Window struct {
	Width  int `yaml:"width" json:"width"`
	Height int `yaml:"height" json:"height"`
}

// Capability is one declared capability's configuration. Only state has a
// setting, its scope; "local" is the one scope in v1.
type Capability struct {
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
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
	// View is the view file's bytes, validated against the manifest
	// (ValidateView): for view: a2ui the view document (JSON text, one JSON
	// object, a valid A2UI view); for view: script the view.js source (UTF-8,
	// within the cap). ViewFileOf names the file.
	View []byte
	// Files is the page file set of a view: html artifact (path relative to
	// html/, sorted), nil for the other kinds. A Put of an html artifact
	// replaces the whole set; nil is refused, never "keep what is there".
	Files []PageFile
	// Page is what the save-time vet derived from Files (static or scripted,
	// the queries the page uses). Set by Get and List; ignored on input.
	Page *PageInfo
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

// ViewFileOf is the name of the view file a manifest's view kind keeps beside
// artifact.yaml.
func ViewFileOf(m *Manifest) string {
	switch m.View {
	case ViewScript:
		return ScriptFile
	case ViewHTML:
		return HTMLDir + "/"
	}
	return ViewFile
}

// ViewLabel is the short text a list shows for an artifact's view kind: the
// manifest's kind, with an html page's derived host in brackets
// ("html (static)", "html (scripted)"). Empty for a broken artifact.
func ViewLabel(a *Artifact) string {
	switch {
	case a.Manifest.View == ViewHTML && a.Page != nil:
		return ViewHTML + " (" + a.Page.Kind + ")"
	case a.Manifest.View == "" && a.Error == "":
		return ViewA2UI
	}
	return a.Manifest.View
}
