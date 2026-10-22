package bundle

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// A data artifact travels as two entries, artifacts/<name>/artifact.yaml and
// its view file (view.a2ui.json for view: a2ui, view.js for view: script), both
// of kind artifact with the same id. They are one item: both present, both
// valid together, and the view file the one the manifest names, or the bundle
// is refused. A bundle never carries an allow: a script view arriving by bundle
// is undecided on the receiving machine and shows as source until allowed.

// artifactPair collects the two entries of one artifact while a bundle opens.
type artifactPair struct {
	manifest, view       []byte
	manifestEnt, viewEnt *Entry
	viewFile             string
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
	manifest, viewFile, view, err := ex.artifactBytes(id)
	if err == nil {
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
	for _, f := range []struct {
		file string
		data []byte
	}{{artifacts.ManifestFile, manifest}, {viewFile, view}} {
		ex.items = append(ex.items, exportItem{Entry{Path: artifacts.Dir + "/" + id + "/" + f.file, Kind: KindArtifact, ID: id, SHA256: sha256Hex(f.data), Size: int64(len(f.data))}, f.data})
	}
	return nil
}

func (ex *exporter) artifactBytes(id string) ([]byte, string, []byte, error) {
	src, err := ex.t.Artifacts().Source(context.Background(), id)
	if err != nil {
		return nil, "", nil, err
	}
	if src.ViewFile == artifacts.HTMLDir+"/" {
		return nil, "", nil, errors.New("an HTML page is not exported yet")
	}
	if src.ManifestMissing || src.ViewMissing {
		return nil, "", nil, errors.New("one of its two files is missing")
	}
	return []byte(src.Manifest), src.ViewFile, []byte(src.View), nil
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
	a := &artifacts.Artifact{Name: id, ManifestText: it.Data, View: it.View}
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
