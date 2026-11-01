package bundle

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/SandwichLabs/slaw/internal/artifacts"
)

// A data artifact travels as two entries, artifacts/<name>/artifact.yaml and
// its view file (view.a2ui.json for view: a2ui, view.js for view: script), both
// of kind artifact with the same id. They are one item: both present, both
// valid together, and the view file the one the manifest names, or the bundle
// is refused. A view: html page travels as artifact.yaml plus one entry per
// file under artifacts/<name>/html/, under H1's rules (paths, extensions,
// caps), and the whole set must pass the page vet together. Everything is
// checked while the bundle opens, before any store is written. A bundle never
// carries an allow: a script view or a page arriving by bundle is undecided on
// the receiving machine and shows as source until allowed. Its state
// (.slaw-local) never travels either: it is not a store a bundle reads.

// artifactPair collects the two entries of one artifact while a bundle opens.
type artifactPair struct {
	manifest, view       []byte
	manifestEnt, viewEnt *Entry
	viewFile             string
	// files are the entries under html/ (path relative to html/).
	files []artifacts.PageFile
}

type artifactParts map[string]*artifactPair

// add records one entry's bytes by its exact file name. An unknown name or a
// slot already filled refuses the bundle (classify already keeps both out;
// this keeps them out if it is ever widened).
func (ap artifactParts) add(e Entry, b []byte) error {
	pr := ap[e.ID]
	if pr == nil {
		pr = &artifactPair{}
		ap[e.ID] = pr
	}
	if rest, ok := strings.CutPrefix(e.Path, artifacts.Dir+"/"+e.ID+"/"+artifacts.HTMLDir+"/"); ok {
		pr.files = append(pr.files, artifacts.PageFile{Path: rest, Data: b})
		return nil
	}
	switch path.Base(e.Path) {
	case artifacts.ManifestFile:
		if pr.manifestEnt != nil {
			return corrupt("artifact %q has two %s", e.ID, artifacts.ManifestFile)
		}
		pr.manifest, pr.manifestEnt = b, &e
	case artifacts.ViewFile, artifacts.ScriptFile:
		if pr.viewEnt != nil {
			return corrupt("artifact %q has two view files", e.ID)
		}
		pr.view, pr.viewEnt, pr.viewFile = b, &e, path.Base(e.Path)
	default:
		return corrupt("unexpected entry %q in an artifact", e.Path)
	}
	return nil
}

// items validates every pair and returns one item per artifact.
func (ap artifactParts) items() ([]item, error) {
	out := make([]item, 0, len(ap))
	for id, pr := range ap {
		if len(pr.files) > 0 {
			it, err := pr.pageItem(id)
			if err != nil {
				return nil, err
			}
			out = append(out, it)
			continue
		}
		if pr.manifestEnt == nil || pr.viewEnt == nil {
			return nil, corrupt("artifact %q needs %s and a view file (%s or %s)", id, artifacts.ManifestFile, artifacts.ViewFile, artifacts.ScriptFile)
		}
		title, err := validateArtifact(id, pr.manifest, pr.viewFile, pr.view)
		if err != nil {
			return nil, err
		}
		out = append(out, item{Entry: *pr.manifestEnt, Title: cleanText(title, maxTextRunes), Data: pr.manifest, View: pr.view})
	}
	return out, nil
}

// pageItem is the item of an artifact that travels as a page: the manifest and
// the files, with no view file, vetted together as the store would.
func (pr *artifactPair) pageItem(id string) (item, error) {
	where := artifacts.Dir + "/" + id
	if pr.manifestEnt == nil || pr.viewEnt != nil {
		return item{}, corrupt("%s: a page needs %s and its html/ files, and no other view file", where, artifacts.ManifestFile)
	}
	title, err := validatePage(id, pr.manifest, pr.files)
	if err != nil {
		return item{}, err
	}
	return item{Entry: *pr.manifestEnt, Title: cleanText(title, maxTextRunes), Data: pr.manifest, Files: pr.files}, nil
}

// validatePage is the structural check of a page: the manifest through Decode
// and the file set through the page vet (H1 and H2), the rules the store
// applies on every write. SQL policy is the agents package's.
func validatePage(id string, manifest []byte, files []artifacts.PageFile) (string, error) {
	where := artifacts.Dir + "/" + id
	page, err := artifacts.PageFromText(id, manifest, files)
	if err != nil {
		return "", corrupt("%s: %v", where, err)
	}
	return page.Manifest.Title, nil
}

// validateArtifact is the structural check: the manifest through
// artifacts.Decode and the view through artifacts.ValidateView, the rules the
// store applies on every write. SQL policy is the agents package's, which this
// one may not import: an imported query that the policy refuses imports, and
// is refused when it runs.
func validateArtifact(id string, manifest []byte, viewFile string, view []byte) (string, error) {
	where := artifacts.Dir + "/" + id
	m, err := artifacts.Decode(manifest)
	if err != nil {
		return "", corrupt("%s: %v", where, err)
	}
	if want := artifacts.ViewFileOf(m); viewFile != want {
		return "", corrupt("%s: view: %s needs %s, not %s", where, m.View, want, viewFile)
	}
	if err := artifacts.ValidateView(m, view); err != nil {
		return "", corrupt("%s: %v", where, err)
	}
	return m.Title, nil
}

// addArtifact exports one artifact from the store's own two files.
func (ex *exporter) addArtifact(id string, explicit bool) error {
	manifest, viewFile, view, files, err := ex.artifactBytes(id)
	if err == nil && files != nil {
		_, err = validatePage(id, manifest, files)
		if err != nil {
			err = errors.New("it is not a valid page (open it in Studio to see why)")
		}
	} else if err == nil {
		_, err = validateArtifact(id, manifest, viewFile, view)
		if err != nil {
			err = errors.New("it is not a valid artifact (open it in Studio to see why)")
		}
	}
	if err != nil {
		if explicit {
			return fmt.Errorf("cannot export %s: %w", Key(KindArtifact, id), err)
		}
		ex.warnings = append(ex.warnings, fmt.Sprintf("%s/%s was skipped: %v", artifacts.Dir, id, err))
		return nil
	}
	type part struct {
		file string
		data []byte
	}
	parts := []part{{artifacts.ManifestFile, manifest}}
	if files != nil {
		for _, f := range files {
			parts = append(parts, part{artifacts.HTMLDir + "/" + f.Path, f.Data})
		}
	} else {
		parts = append(parts, part{viewFile, view})
	}
	for _, f := range parts {
		ex.items = append(ex.items, exportItem{Entry{Path: artifacts.Dir + "/" + id + "/" + f.file, Kind: KindArtifact, ID: id, SHA256: sha256Hex(f.data), Size: int64(len(f.data))}, f.data})
	}
	return nil
}

// artifactBytes reads an artifact's files for export: the manifest, and the
// view file, or for a page its files (non-nil, never empty).
func (ex *exporter) artifactBytes(id string) (manifest []byte, viewFile string, view []byte, files []artifacts.PageFile, err error) {
	src, err := ex.t.Artifacts().Source(context.Background(), id)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if src.ManifestMissing || src.ViewMissing {
		return nil, "", nil, nil, errors.New("one of its two files is missing")
	}
	if src.ViewFile == artifacts.HTMLDir+"/" {
		if len(src.Files) == 0 {
			return nil, "", nil, nil, errors.New("its page has no files")
		}
		return []byte(src.Manifest), "", nil, src.Files, nil
	}
	return []byte(src.Manifest), src.ViewFile, []byte(src.View), nil, nil
}

// artifactRev is the revision (over both files) of the artifact in the
// workspace now, or "".
func artifactRev(ctx context.Context, t Target, id string) string {
	src, err := t.Artifacts().Source(ctx, id)
	if err != nil {
		return ""
	}
	return src.Rev
}

// writeArtifact stores one artifact under id through the store: a new one only
// creates, an overwrite is a compare-and-write against the revision read at
// preview.
func (p *Package) writeArtifact(ctx context.Context, t Target, it item, id string, overwrite bool) (string, error) {
	a := &artifacts.Artifact{Name: id, ManifestText: it.Data, View: it.View, Files: it.Files}
	opts := []artifacts.PutOption{artifacts.WithRev("")}
	if overwrite {
		rev := p.revFor(ctx, t, KindArtifact, id)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing artifact %q: it could not be read", id)
		}
		opts = []artifacts.PutOption{artifacts.WithRev(rev)}
	}
	if err := t.Artifacts().Put(ctx, a, opts...); err != nil {
		return "", err
	}
	return a.Name, nil
}
