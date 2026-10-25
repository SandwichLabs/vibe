package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
)

// The editing side of data artifacts (#162, step 6a): what the Source tab reads
// and writes. Nothing here is a binding; app_artifacts.go calls it.

// ArtifactSource is an artifact's two files as text, as they are on disk, valid
// or not (a broken artifact can be opened and fixed). Rev covers both files and
// is what SaveArtifact and ResolveConflict send back.
type ArtifactSource struct {
	Name     string `json:"name"`
	Manifest string `json:"manifest"`
	View     string `json:"view"`
	// ViewFile names the view file ("view.a2ui.json" or "view.js").
	ViewFile        string `json:"view_file"`
	ManifestMissing bool   `json:"manifest_missing"`
	ViewMissing     bool   `json:"view_missing"`
	Rev             string `json:"rev"`
	UpdatedAt       string `json:"updated_at"`
}

// ArtifactSaveResult is a save that landed. Warnings are queries the agent's
// read policy refuses (the artifact is saved anyway: a person must be able to
// save while a source is offline, and the same gate refuses the run).
type ArtifactSaveResult struct {
	Rev       string   `json:"rev"`
	UpdatedAt string   `json:"updated_at"`
	Warnings  []string `json:"warnings"`
}

// artifactEnv is what editing needs, taken from one workspace snapshot: the
// store, the vetter of that same workspace, and its root (to keep absolute
// paths out of what is shown).
type artifactEnv struct {
	store artifacts.Store
	vet   artifactVetter
	root  string
}

// artifactVetter is the SQL policy check of a manifest (agents.Handler.VetArtifact).
type artifactVetter interface {
	VetArtifact(ctx context.Context, m *artifacts.Manifest) error
}

// validArtifactName refuses a name that is not a usable folder name before it
// reaches a store.
func validArtifactName(name string) error {
	if !filestore.ValidID(name) {
		return fmt.Errorf("artifact names are 1 to %d lowercase letters, digits, - or _ (got %q)", artifacts.MaxNameLength, name)
	}
	return nil
}

func getArtifactSource(ctx context.Context, store artifacts.Store, name string) (*ArtifactSource, error) {
	if err := validArtifactName(name); err != nil {
		return nil, err
	}
	src, err := store.Source(ctx, name)
	if errors.Is(err, artifacts.ErrNotFound) {
		return nil, fmt.Errorf("there is no artifact named %q", name)
	}
	if err != nil {
		return nil, err
	}
	if src.ViewFile == "" {
		src.ViewFile = artifacts.ViewFile
	}
	return &ArtifactSource{
		Name: src.Name, Manifest: src.Manifest, View: src.View, ViewFile: src.ViewFile, ManifestMissing: src.ManifestMissing,
		ViewMissing: src.ViewMissing, Rev: src.Rev, UpdatedAt: src.UpdatedAt,
	}, nil
}

// saveArtifact stores a person's edit. Structure decides: a manifest or view
// that does not pass is refused and nothing is written, and a stale rev is a
// conflict (either file changed). The files are written as typed, so the
// manifest keeps its comments. Only then is the SQL policy asked, and its
// refusals come back as warnings with the policy's reason.
func saveArtifact(ctx context.Context, env artifactEnv, name, manifest, view, rev string) (*ArtifactSaveResult, error) {
	if err := validArtifactName(name); err != nil {
		return nil, err
	}
	m, err := artifacts.Decode([]byte(manifest))
	if err != nil {
		return nil, err
	}
	a := &artifacts.Artifact{Name: name, Manifest: *m, ManifestText: []byte(manifest), View: []byte(view)}
	if err := env.store.Put(ctx, a, artifacts.WithRev(rev)); err != nil {
		return nil, err
	}
	return &ArtifactSaveResult{Rev: a.Rev, UpdatedAt: a.UpdatedAt, Warnings: vetWarnings(ctx, env, &a.Manifest)}, nil
}

// vetWarnings is the policy's reason for each query it refuses, one string
// each, never nil. The reasons are the agent's own (credential patterns are
// already redacted by VetArtifact); the workspace's absolute path is replaced
// with <workspace>. A vet that could not run at all (the workspace was
// switched or closed) says so in a fixed sentence.
func vetWarnings(ctx context.Context, env artifactEnv, m *artifacts.Manifest) []string {
	err := env.vet.VetArtifact(ctx, m)
	if err == nil {
		return []string{}
	}
	var ve *agents.ArtifactVetError
	if errors.As(err, &ve) {
		out := make([]string, 0, len(ve.Problems))
		for _, p := range ve.Problems {
			if env.root != "" {
				p = strings.ReplaceAll(p, env.root, "<workspace>")
			}
			out = append(out, p)
		}
		return out
	}
	return []string{"The queries could not be checked against the agent's read policy just now. They are checked again when the artifact runs."}
}

// checkArtifact vets the stored artifact (what a run would be held to) and
// returns the warnings; a broken artifact has none to give, its error shows.
func checkArtifact(ctx context.Context, env artifactEnv, name string) ([]string, error) {
	if err := validArtifactName(name); err != nil {
		return nil, err
	}
	a, err := env.store.Get(ctx, name)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotFound) {
			return nil, fmt.Errorf("there is no artifact named %q", name)
		}
		return []string{}, nil
	}
	return vetWarnings(ctx, env, &a.Manifest), nil
}

// newArtifact creates artifacts/<name> from the starter of kind (see
// artifact_starters.go) and returns its source. An existing name is refused
// (create-only), never overwritten; so is a kind that is not a starter. The
// store vets the page files and the view on the way in, and a starter that
// fails it is a bug the tests catch.
func newArtifact(ctx context.Context, store artifacts.Store, name, kind string) (*ArtifactSource, error) {
	if err := validArtifactName(name); err != nil {
		return nil, err
	}
	st, err := starterFor(name, kind)
	if err != nil {
		return nil, err
	}
	m, err := artifacts.Decode([]byte(st.manifest))
	if err != nil {
		return nil, err
	}
	a := &artifacts.Artifact{Name: name, Manifest: *m, ManifestText: []byte(st.manifest), View: []byte(st.view), Files: st.files}
	if err := vetFreshPage(m, st.files); err != nil {
		return nil, err
	}
	if err := store.Put(ctx, a, artifacts.WithRev("")); err != nil {
		if errors.Is(err, artifacts.ErrConflict) {
			return nil, fmt.Errorf("an artifact named %q already exists", name)
		}
		return nil, err
	}
	return &ArtifactSource{
		Name: name, Manifest: st.manifest, View: st.view, ViewFile: artifacts.ViewFileOf(m), Rev: a.Rev,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// vetFreshPage holds page files a person is creating now to the write-time
// rules (artifacts.VetPageForWrite). Stored pages, bundle imports and conflict
// resolution go through the store's lenient load-time vet instead, so a page
// already on disk or in a bundle is never refused for an old mistake.
func vetFreshPage(m *artifacts.Manifest, files []artifacts.PageFile) error {
	if m.View != artifacts.ViewHTML {
		return nil
	}
	_, err := artifacts.VetPageForWrite(m, files)
	return err
}

// deleteArtifact removes an artifact if it is still at rev. A delete without a
// rev is refused: the caller must say what it saw, or a delete races an edit
// that landed after the person last looked.
func deleteArtifact(ctx context.Context, store artifacts.Store, name, rev string) error {
	if err := validArtifactName(name); err != nil {
		return err
	}
	if rev == "" {
		return errors.New("reopen the artifact before deleting it: its revision is unknown")
	}
	return store.Delete(ctx, name, artifacts.WithRev(rev))
}
