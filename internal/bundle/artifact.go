package bundle

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// A data artifact travels as two entries, artifacts/<name>/artifact.yaml and
// artifacts/<name>/view.a2ui.json, both of kind artifact with the same id. They
// are one item: both present, both valid together, or the bundle is refused.
// There is no view.html entry until custom views exist.

// artifactPair collects the two entries of one artifact while a bundle opens.
type artifactPair struct {
	manifest, view       []byte
	manifestEnt, viewEnt *Entry
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
	case artifacts.ViewFile:
		if pr.viewEnt != nil {
			return corrupt("artifact %q has two %s", e.ID, artifacts.ViewFile)
		}
		pr.view, pr.viewEnt = b, &e
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
			return nil, corrupt("artifact %q needs both %s and %s", id, artifacts.ManifestFile, artifacts.ViewFile)
		}
		title, err := validateArtifact(id, pr.manifest, pr.view)
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
func validateArtifact(id string, manifest, view []byte) (string, error) {
	where := artifacts.Dir + "/" + id
	m, err := artifacts.Decode(manifest)
	if err != nil {
		return "", corrupt("%s: %v", where, err)
	}
	if err := artifacts.ValidateView(m, view); err != nil {
		return "", corrupt("%s: %v", where, err)
	}
	return m.Title, nil
}

// addArtifact exports one artifact from the store's own two files.
func (ex *exporter) addArtifact(id string, explicit bool) error {
	manifest, view, err := ex.artifactBytes(id)
	if err == nil {
		_, err = validateArtifact(id, manifest, view)
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
	}{{artifacts.ManifestFile, manifest}, {artifacts.ViewFile, view}} {
		ex.items = append(ex.items, exportItem{Entry{Path: artifacts.Dir + "/" + id + "/" + f.file, Kind: KindArtifact, ID: id, SHA256: sha256Hex(f.data), Size: int64(len(f.data))}, f.data})
	}
	return nil
}

func (ex *exporter) artifactBytes(id string) ([]byte, []byte, error) {
	src, err := ex.t.Artifacts().Source(context.Background(), id)
	if err != nil {
		return nil, nil, err
	}
	if src.ManifestMissing || src.ViewMissing {
		return nil, nil, errors.New("one of its two files is missing")
	}
	return []byte(src.Manifest), []byte(src.View), nil
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
