package bundle

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/skills"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// Manifest identity.
const (
	Kind          = "vibe/bundle"
	FormatVersion = 2

	// ManifestName and ConnectionsName are the two fixed entries.
	ManifestName    = "manifest.json"
	ConnectionsName = "connections.yaml"
)

// Caps. A bundle is text; these are generous for notebooks and skills and
// small enough that an unpacked bundle can be held in memory.
const (
	// MaxEntryBytes caps one unpacked entry (the stores' 10 MB per file).
	MaxEntryBytes = 10 << 20
	// MaxTotalBytes caps the unpacked size of everything in one bundle.
	MaxTotalBytes = 50 << 20
	// MaxFileBytes caps the file as read from disk (compressed, or encrypted).
	MaxFileBytes = MaxTotalBytes + 16<<20
	// MaxEntries caps the number of entries.
	MaxEntries = 2000

	maxManifestBytes = 4 << 20
)

// Errors a caller can test with errors.Is.
var (
	// ErrLegacy is a formatVersion 1 or SQLCipher .vibe file: one that holds
	// data. Vibe no longer opens these.
	ErrLegacy = errors.New("This is an older .vibe file that holds data. Vibe no longer opens these.")
	// ErrNotBundle is a file that is not a Vibe bundle at all.
	ErrNotBundle = errors.New("this is not a Vibe bundle (an older .vibe file that held data is not opened either)")
	// ErrPassphraseRequired means the bundle is encrypted and no passphrase was given.
	ErrPassphraseRequired = errors.New("this bundle is encrypted: a passphrase is required")
	// ErrWrongPassphrase means the passphrase did not open the bundle.
	ErrWrongPassphrase = errors.New("wrong passphrase: that passphrase does not open this bundle")
	// ErrUnsupportedKDF means the envelope asks for more passphrase-hashing
	// work than Vibe will do.
	ErrUnsupportedKDF = errors.New("this bundle was made with settings Vibe won't use")
	// ErrTooLarge means a size cap was exceeded.
	ErrTooLarge = errors.New("bundle is too large")
	// ErrCorrupt means the bundle failed verification: a checksum, an unknown
	// or unsafe entry, a malformed document.
	ErrCorrupt = errors.New("bundle is damaged or has been tampered with")
)

func corrupt(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, a...))
}

// ItemKind is what an entry is.
type ItemKind string

// Item kinds.
const (
	KindNotebook    ItemKind = "notebook"
	KindQuery       ItemKind = "query"
	KindSkill       ItemKind = "skill"
	KindConnections ItemKind = "connections"
)

// Entry is one manifest line.
type Entry struct {
	Path   string   `json:"path"`
	Kind   ItemKind `json:"kind"`
	ID     string   `json:"id,omitempty"`
	SHA256 string   `json:"sha256"`
	Size   int64    `json:"size"`
}

// Manifest is manifest.json. It never carries titles or content: a preview
// derives those from the verified entries themselves.
type Manifest struct {
	Kind          string   `json:"kind"`
	FormatVersion int      `json:"formatVersion"`
	CreatedAt     string   `json:"createdAt"`
	CreatedBy     string   `json:"createdBy"`
	WorkspaceName string   `json:"workspaceName"`
	Contents      []Entry  `json:"contents"`
	Warnings      []string `json:"warnings,omitempty"`
}

// Target is the workspace a bundle is exported from or imported into.
// workspace.Workspace satisfies it.
type Target interface {
	Root() string
	Name() string
	Config() workspace.Config
	Local() workspace.LocalConfig
	Connections() []workspace.Connection
	Connection(name string) (workspace.Connection, bool)
	Notebooks() notebooks.Store
	Queries() queries.Store
	Skills() skills.Store
	AddConnection(scope workspace.Scope, def workspace.ConnectionDef) error
	RemoveConnection(ctx context.Context, scope workspace.Scope, name string) error
}

var _ Target = workspace.Workspace(nil)

// Selection says what an export takes. Notebooks, Queries and Skills are ids
// (skill names); All takes every one of each kind and ignores them.
type Selection struct {
	All       bool     `json:"all"`
	Notebooks []string `json:"notebooks"`
	Queries   []string `json:"queries"`
	Skills    []string `json:"skills"`
	// SharedConnections adds every committed (team) vibe.yaml definition;
	// TeamConnections names just some of them.
	SharedConnections bool     `json:"sharedConnections"`
	TeamConnections   []string `json:"teamConnections"`
	// LocalConnections names "This computer" definitions the person ticked.
	LocalConnections []string `json:"localConnections"`
}

// ExportOptions are the inputs of Export.
type ExportOptions struct {
	Selection Selection
	// Passphrase, when non-empty, wraps the whole bundle in an age envelope.
	Passphrase string
	// AppVersion is recorded as createdBy.
	AppVersion string
	// Now overrides the clock (tests); zero means time.Now.
	Now func() string
}

// ExportResult is a finished bundle.
type ExportResult struct {
	Data      []byte   `json:"-"`
	Manifest  Manifest `json:"manifest"`
	Encrypted bool     `json:"encrypted"`
}

// --- entry names ---

// classify maps an entry name to its kind and id, or refuses it. The set of
// accepted names is closed: this is what keeps a hostile archive from writing
// anywhere but the stores' own files.
func classify(name string) (ItemKind, string, error) {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") {
		return "", "", corrupt("unsafe entry name %q", name)
	}
	if path.Clean(name) != name {
		return "", "", corrupt("unsafe entry name %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", "", corrupt("unsafe entry name %q", name)
		}
	}
	switch name {
	case ManifestName:
		return "", "", nil
	case ConnectionsName:
		return KindConnections, "", nil
	}
	parts := strings.Split(name, "/")
	switch {
	case len(parts) == 2 && parts[0] == notebooks.Dir && strings.HasSuffix(parts[1], notebooks.Extension):
		id := strings.TrimSuffix(parts[1], notebooks.Extension)
		if filestore.ValidID(id) {
			return KindNotebook, id, nil
		}
	case len(parts) == 2 && parts[0] == queries.Dir && strings.HasSuffix(parts[1], queries.Extension):
		id := strings.TrimSuffix(parts[1], queries.Extension)
		if filestore.ValidID(id) {
			return KindQuery, id, nil
		}
	case len(parts) == 3 && parts[0] == skills.Dir && parts[2] == skills.File:
		if skills.NamePattern.MatchString(parts[1]) && filestore.ValidID(parts[1]) {
			return KindSkill, parts[1], nil
		}
	}
	return "", "", corrupt("unexpected entry %q (a bundle holds only notebooks, queries, skills and connection definitions)", name)
}

func entryPath(kind ItemKind, id string) string {
	switch kind {
	case KindNotebook:
		return notebooks.Dir + "/" + id + notebooks.Extension
	case KindQuery:
		return queries.Dir + "/" + id + queries.Extension
	case KindSkill:
		return skills.Dir + "/" + id + "/" + skills.File
	}
	return ConnectionsName
}

// Key identifies an item in previews and decisions: "notebook/<id>".
func Key(kind ItemKind, id string) string { return string(kind) + "/" + id }
