package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
)

// artifactConflicts resolves a conflict on a data artifact. The item is two
// files with one revision (artifacts.Store), so "the file changed" means either
// of them, and every action works on the pair.
type artifactConflicts struct{ store artifacts.Store }

// maxCopyAttempts bounds the search for a free "<name>-copy-N".
const maxCopyAttempts = 100

// current is the artifact's files on disk, nil when it is gone. Unlike Get it
// reads a broken artifact too, so a conflict on one can still be resolved.
func (c artifactConflicts) current(ctx context.Context, name string) (*artifacts.Source, error) {
	src, err := c.store.Source(ctx, name)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return nil, err
	}
	return src, nil
}

// title is the manifest's title, or the name when the manifest cannot be read.
func artifactTitle(manifest, name string) string {
	if m, err := artifacts.Decode([]byte(manifest)); err == nil && m.Title != "" {
		return m.Title
	}
	return name
}

// edited builds the artifact to write from the editor's two texts. Both go to
// disk as typed (the manifest keeps its comments); the structural checks are
// the store's.
func (c artifactConflicts) edited(name string, req ConflictRequest) (*artifacts.Artifact, error) {
	manifest, view := str(req.Data, "manifest"), str(req.Data, "view")
	m, err := artifacts.Decode([]byte(manifest))
	if err != nil {
		return nil, err
	}
	return &artifacts.Artifact{Name: name, Manifest: *m, ManifestText: []byte(manifest), View: []byte(view)}, nil
}

func (c artifactConflicts) result(a *artifacts.Artifact) *ConflictResult {
	return &ConflictResult{ID: a.Name, Rev: a.Rev, Title: a.Manifest.Title}
}

func (c artifactConflicts) reload(ctx context.Context, name string) (*ConflictResult, error) {
	cur, err := c.current(ctx, name)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, artifacts.ErrNotFound
	}
	return &ConflictResult{ID: cur.Name, Rev: cur.Rev, Title: artifactTitle(cur.Manifest, cur.Name)}, nil
}

func (c artifactConflicts) overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	a, err := c.edited(req.ID, req)
	if err != nil {
		return nil, err
	}
	cur, err := c.current(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	var opt artifacts.PutOption
	switch {
	case cur == nil:
		opt = artifacts.Overwrite() // deleted on disk: write it back
	case req.Rev != "":
		opt = artifacts.WithRev(req.Rev) // only the revision the banner was raised against
	default:
		// No revision known and something is there (recreated since the banner,
		// or the revision never arrived): not ours to replace unseen. The
		// banner re-raises and fetches the revision.
		return nil, &filestore.ConflictError{Path: artifacts.Dir + "/" + req.ID, Got: cur.Rev}
	}
	if err := c.store.Put(ctx, a, opt); err != nil {
		return nil, err
	}
	return c.result(a), nil
}

// saveCopy writes the edit as "<name>-copy", or the first free
// "<name>-copy-N". Each attempt is a create-only Put, so a name taken meanwhile
// (by anyone) is a conflict that moves on to the next, never an overwrite.
func (c artifactConflicts) saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	first, err := c.edited(artifactCopyName(req.ID, 1), req)
	if err != nil {
		return nil, err
	}
	if err := artifacts.ValidateView(&first.Manifest, first.View); err != nil {
		return nil, err // structural: no name would help
	}
	for n := 1; n < maxCopyAttempts; n++ {
		a, err := c.edited(artifactCopyName(req.ID, n), req)
		if err != nil {
			return nil, err
		}
		err = c.store.Put(ctx, a, artifacts.WithRev(""))
		// Taken (conflict), or a name the store cannot use (a link or a plain file
		// in its place): move on to the next.
		if errors.Is(err, artifacts.ErrConflict) || errors.Is(err, artifacts.ErrInvalid) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return c.result(a), nil
	}
	return nil, fmt.Errorf("could not find a free name for a copy of %q", req.ID)
}

// artifactCopyName is "<id>-copy" for n == 1 and "<id>-copy-<n>" after, with
// id shortened so the result is still a valid artifact name.
func artifactCopyName(id string, n int) string {
	suffix := "-copy"
	if n > 1 {
		suffix = fmt.Sprintf("-copy-%d", n)
	}
	if room := artifacts.MaxNameLength - len(suffix); len(id) > room {
		id = id[:room]
	}
	return id + suffix
}
