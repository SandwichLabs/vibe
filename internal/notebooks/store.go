package notebooks

import (
	"context"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Store is where a workspace keeps its notebooks (plan §4.1). Two
// implementations exist: the file store (notebooks/<id>.vnb in a workspace
// folder, with compare-and-write) and the SQLite store that wraps the legacy
// container's _vibe_notebooks table so `.vibe` files keep working.
//
// Every method goes through Coerce on load and save, so a notebook is always
// rebuilt field by field from a whitelist no matter who wrote the bytes.
type Store interface {
	// List returns every notebook, newest-updated first.
	List(ctx context.Context) ([]Summary, error)
	// Get loads one notebook by id, with coercion warnings. Nb.Rev carries
	// the revision to hand back on Save.
	Get(ctx context.Context, id string) (Notebook, []string, error)
	// Save coerces raw (JSON string/bytes or a decoded value) and writes it.
	// With a file store a stale write, one whose revision (raw's "rev",
	// IfRev, or the revision this store last saw for the id) no longer
	// matches the file, returns an error satisfying errors.Is(err,
	// ErrConflict) and changes nothing.
	Save(ctx context.Context, raw any, opts ...SaveOption) (Notebook, []string, error)
	// Create makes a new, empty notebook with the given title.
	Create(ctx context.Context, title string) (Notebook, error)
	// Insert stores a notebook built in code (a template, a migration).
	Insert(ctx context.Context, nb Notebook) (Notebook, []string, error)
	// Rename changes the title only. A file store keeps the file name, so the
	// id stays stable.
	Rename(ctx context.Context, id, title string) error
	// Delete removes a notebook; a missing id is a no-op.
	Delete(ctx context.Context, id string) error
}

// ErrConflict is returned (wrapped) by a file store when the notebook changed
// on disk since it was loaded: a git pull, an editor, another process.
var ErrConflict = filestore.ErrConflict

// ErrNotFound is returned (wrapped) by Get for an unknown id.
var ErrNotFound = filestore.ErrNotFound

// SaveOption tunes one Save.
type SaveOption func(*saveOpts)

type saveOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
	keepID    bool
}

// Overwrite skips the conflict check: the caller has chosen "overwrite" in the
// conflict banner.
func Overwrite() SaveOption { return func(o *saveOpts) { o.overwrite = true } }

// KeepID stores the notebook under its own id even when that id looks like a
// generated one (nb_<uuid>), which Save otherwise replaces with a name taken
// from the title. A bundle import uses it so that an id means the same thing
// in the bundle and in the workspace.
func KeepID() SaveOption { return func(o *saveOpts) { o.keepID = true } }

// IfRev sets the revision the caller loaded, overriding the notebook's own
// "rev" field.
func IfRev(rev string) SaveOption {
	return func(o *saveOpts) { o.rev, o.hasRev = rev, true }
}

func applySaveOpts(opts []SaveOption) saveOpts {
	var o saveOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}
