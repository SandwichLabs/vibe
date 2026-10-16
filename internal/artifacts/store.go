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
	// Delete removes an artifact's files (and its folder when that leaves it
	// empty); a missing name is a no-op.
	Delete(ctx context.Context, name string) error
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

// WithRev sets the revision the caller loaded, overriding a.Rev.
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
	manifest, err := Encode(&a.Manifest)
	if err != nil {
		return prepared{}, err
	}
	if err := ValidateView(&a.Manifest, a.View); err != nil {
		return prepared{}, err
	}
	return prepared{manifest: manifest, view: a.View}, nil
}
