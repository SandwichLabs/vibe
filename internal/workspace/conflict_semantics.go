package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/semantics"
)

// semanticsConflicts resolves a save of a file under semantics/ that was
// refused because it changed on disk. The ID is the semantics id
// ("entities/customer", "metrics/net_revenue", "joins/orders_customers" or
// "glossary") and Data carries the local edit as {text}: the file's text.
type semanticsConflicts struct{ store semantics.Store }

// split reads an id into its kind and name.
func (c semanticsConflicts) split(id string) (semantics.Kind, string, error) {
	if id == "glossary" {
		return semantics.KindGlossary, "", nil
	}
	folder, name, _ := strings.Cut(id, "/")
	switch folder {
	case semantics.EntitiesDir:
		return semantics.KindEntity, name, nil
	case semantics.MetricsDir:
		return semantics.KindMetric, name, nil
	case semantics.JoinsDir:
		return semantics.KindJoin, name, nil
	}
	return "", "", fmt.Errorf("unknown semantics id %q", id)
}

func (c semanticsConflicts) reload(ctx context.Context, id string) (*ConflictResult, error) {
	kind, name, err := c.split(id)
	if err != nil {
		return nil, err
	}
	src, err := c.store.Source(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: id, Rev: src.Rev, Title: id}, nil
}

func (c semanticsConflicts) overwrite(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	kind, name, err := c.split(req.ID)
	if err != nil {
		return nil, err
	}
	var opts []semantics.WriteOption
	switch cur, serr := c.store.Source(ctx, kind, name); {
	case errors.Is(serr, semantics.ErrNotFound):
		// deleted on disk: write it back (a create)
	case serr != nil:
		return nil, serr
	case req.Rev != "":
		opts = append(opts, semantics.WithRev(req.Rev)) // only the revision the banner was raised against
	default:
		opts = append(opts, semantics.WithRev(cur.Rev))
	}
	rev, err := c.store.PutSource(ctx, kind, name, []byte(str(req.Data, "text")), opts...)
	if err != nil {
		return nil, err
	}
	return &ConflictResult{ID: req.ID, Rev: rev, Title: req.ID}, nil
}

func (c semanticsConflicts) saveCopy(ctx context.Context, req ConflictRequest) (*ConflictResult, error) {
	kind, name, err := c.split(req.ID)
	if err != nil {
		return nil, err
	}
	if kind == semantics.KindGlossary {
		return nil, errors.New("the glossary is one file and cannot be saved as a copy")
	}
	text := []byte(str(req.Data, "text"))
	for n := 1; n < 100; n++ {
		copyName := name + "_copy"
		if n > 1 {
			copyName = fmt.Sprintf("%s_copy_%d", name, n)
		}
		if _, serr := c.store.Source(ctx, kind, copyName); !errors.Is(serr, semantics.ErrNotFound) {
			if serr != nil {
				return nil, serr
			}
			continue // never overwrite an earlier copy
		}
		rev, err := c.putCopy(ctx, kind, name, copyName, text)
		if err != nil {
			return nil, err
		}
		folder := map[semantics.Kind]string{semantics.KindEntity: semantics.EntitiesDir, semantics.KindMetric: semantics.MetricsDir, semantics.KindJoin: semantics.JoinsDir}[kind]
		id := folder + "/" + copyName
		return &ConflictResult{ID: id, Rev: rev, Title: id}, nil
	}
	return nil, errors.New("cannot find a free name for the copy")
}

// putCopy writes the edited text under a new name: the document is decoded
// (the edit must be valid), renamed and written from its decoded form.
func (c semanticsConflicts) putCopy(ctx context.Context, kind semantics.Kind, name, copyName string, text []byte) (string, error) {
	switch kind {
	case semantics.KindEntity:
		e, err := semantics.DecodeEntity(name, text)
		if err != nil {
			return "", err
		}
		e.Name = copyName
		err = c.store.PutEntity(ctx, e)
		return e.Rev, err
	case semantics.KindMetric:
		m, err := semantics.DecodeMetric(name, text)
		if err != nil {
			return "", err
		}
		m.Name = copyName
		err = c.store.PutMetric(ctx, m)
		return m.Rev, err
	}
	j, err := semantics.DecodeJoin(name, text)
	if err != nil {
		return "", err
	}
	j.Name = copyName
	err = c.store.PutJoin(ctx, j)
	return j.Rev, err
}
