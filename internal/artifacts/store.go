package artifacts

import (
	"context"
)

// Store is where a workspace keeps its data artifacts. The file store keeps
// each as artifacts/<name>/artifact.yaml plus artifacts/<name>/view.a2ui.json;
// the Mock keeps them in memory for tests of packages that depend on one.
//
// An artifact is one unit: Rev covers both files, a Put checks it against both
// before writing either, and Get returns the pair.
type Store interface {
	// List returns every artifact ordered by name; never nil. A folder that
	// cannot be read as an artifact is returned with Error set.
	List(ctx context.Context) ([]Artifact, error)
	// Get returns one artifact. A missing name satisfies
	// errors.Is(err, ErrNotFound); a broken one (bad file, symbolic link) is an
	// error.
	Get(ctx context.Context, name string) (*Artifact, error)
	// Put creates or replaces an artifact after validating it. A stale write
	// (a.Rev, WithRev, or the revision last seen for the name no longer
	// matches either file) returns ErrConflict and changes nothing; writing
	// blind over an existing artifact this store never read is also a
	// conflict, so callers Get first (or pass Overwrite). On success a.Rev and
	// a.UpdatedAt are updated.
	Put(ctx context.Context, a *Artifact, opts ...PutOption) error
	// Source returns an artifact's two files as text, as they are on disk,
	// valid or not, with the one revision over both. It is what an editor
	// shows: a broken artifact (which Get refuses) can be read and fixed. A
	// name with neither file satisfies errors.Is(err, ErrNotFound).
	Source(ctx context.Context, name string) (*Source, error)
	// Delete removes an artifact's files (and its folder when that leaves it
	// empty); a missing name is a no-op. It returns ErrConflict when the pair
	// changed since the revision the caller loaded (WithRev), or, without one,
	// since the revision this store last read.
	Delete(ctx context.Context, name string, opts ...PutOption) error
}

// Source is an artifact's files as text, unvalidated. A file that does not
// exist is empty with its Missing flag set. Rev covers both, as for Get.
type Source struct {
	Name            string
	Manifest        string
	View            string
	ManifestMissing bool
	ViewMissing     bool
	Rev             string
	UpdatedAt       string
}

// PutOption tunes one Put.
type PutOption func(*putOpts)

type putOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
}

// Overwrite skips the conflict check ("overwrite" in the conflict banner).
func Overwrite() PutOption { return func(o *putOpts) { o.overwrite = true } }

// WithRev sets the revision the caller loaded, overriding a.Rev. WithRev("")
// says the caller loaded nothing: the Put only creates (an existing artifact is
// a conflict), and the revision the store last saw is NOT used in its place.
// Callers that did not themselves read the artifact (the agent tools) pass it.
func WithRev(rev string) PutOption {
	return func(o *putOpts) { o.rev, o.hasRev = rev, true }
}

func applyPutOpts(opts []PutOption) putOpts {
	var o putOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

// prepared is an artifact ready to write: both files' bytes, validated.
type prepared struct {
	manifest []byte
	view     []byte
}

// prepare validates a for storing and renders its two files. The name rule
// lives in the stores (they differ in how a bad name is reported).
func prepare(a *Artifact) (prepared, error) {
	var manifest []byte
	if len(a.ManifestText) > 0 {
		m, err := Decode(a.ManifestText)
		if err != nil {
			return prepared{}, err
		}
		a.Manifest, manifest = *m, append([]byte(nil), a.ManifestText...)
	} else {
		var err error
		if manifest, err = Encode(&a.Manifest); err != nil {
			return prepared{}, err
		}
	}
	if err := ValidateView(&a.Manifest, a.View); err != nil {
		return prepared{}, err
	}
	return prepared{manifest: manifest, view: a.View}, nil
}
