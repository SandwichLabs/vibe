package bundle

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/semantics"
)

// A semantics item's id is "<folder>/<name>" ("metrics/net_revenue") or
// "glossary". These helpers are the only place the bundle looks inside it.

// splitSemanticsID separates the folder from the name; the glossary has
// neither.
func splitSemanticsID(id string) (folder, name string) {
	folder, name, _ = strings.Cut(id, "/")
	if id == "glossary" {
		return "", ""
	}
	return folder, name
}

// validateSemantics decodes one semantics entry with the store's own decoder,
// so a file that would list with an error in the workspace refuses the whole
// bundle instead. The title is what the preview shows.
func validateSemantics(id string, data []byte) (string, error) {
	folder, name := splitSemanticsID(id)
	switch folder {
	case semantics.EntitiesDir:
		e, err := semantics.DecodeEntity(name, data)
		if err != nil {
			return "", err
		}
		return e.Name, nil
	case semantics.MetricsDir:
		m, err := semantics.DecodeMetric(name, data)
		if err != nil {
			return "", err
		}
		if m.Title != "" {
			return m.Title, nil
		}
		return m.Name, nil
	case semantics.JoinsDir:
		j, err := semantics.DecodeJoin(name, data)
		if err != nil {
			return "", err
		}
		return j.Name, nil
	}
	if _, err := semantics.DecodeGlossary(data); err != nil {
		return "", err
	}
	return "Glossary", nil
}

// semanticsRev is the revision of the semantics file now in the workspace, ""
// when there is none or it cannot be read.
func semanticsRev(ctx context.Context, t Target, id string) string {
	folder, name := splitSemanticsID(id)
	s := t.Semantics()
	switch folder {
	case semantics.EntitiesDir:
		if e, err := s.Entity(ctx, name); err == nil {
			return e.Rev
		}
	case semantics.MetricsDir:
		if m, err := s.Metric(ctx, name); err == nil {
			return m.Rev
		}
	case semantics.JoinsDir:
		if j, err := s.Join(ctx, name); err == nil {
			return j.Rev
		}
	default:
		if g, err := s.Glossary(ctx); err == nil && g.Error == "" {
			return g.Rev
		}
	}
	return ""
}

// semanticsIDs lists the ids of everything under semantics/ that an export can
// take, unreadable files included (validation then skips or refuses them).
func semanticsIDs(ctx context.Context, t Target) ([]string, error) {
	var ids []string
	s := t.Semantics()
	es, err := s.Entities(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.Name != semantics.TruncatedStubName {
			ids = append(ids, semantics.EntitiesDir+"/"+e.Name)
		}
	}
	ms, err := s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.Name != semantics.TruncatedStubName {
			ids = append(ids, semantics.MetricsDir+"/"+m.Name)
		}
	}
	js, err := s.Joins(ctx)
	if err != nil {
		return nil, err
	}
	for _, j := range js {
		if j.Name != semantics.TruncatedStubName {
			ids = append(ids, semantics.JoinsDir+"/"+j.Name)
		}
	}
	g, err := s.Glossary(ctx)
	if err != nil {
		return nil, err
	}
	if g.Rev != "" {
		ids = append(ids, "glossary")
	}
	return ids, nil
}

// semanticsItems lists the items ContentsOf offers: every readable definition.
func semanticsItems(ctx context.Context, t Target) ([]ContentItem, error) {
	out := []ContentItem{}
	s := t.Semantics()
	es, err := s.Entities(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.Error == "" {
			out = append(out, ContentItem{ID: semantics.EntitiesDir + "/" + e.Name, Title: e.Name})
		}
	}
	ms, err := s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.Error == "" {
			title := m.Title
			if title == "" {
				title = m.Name
			}
			out = append(out, ContentItem{ID: semantics.MetricsDir + "/" + m.Name, Title: title})
		}
	}
	js, err := s.Joins(ctx)
	if err != nil {
		return nil, err
	}
	for _, j := range js {
		if j.Error == "" {
			out = append(out, ContentItem{ID: semantics.JoinsDir + "/" + j.Name, Title: j.Name})
		}
	}
	g, err := s.Glossary(ctx)
	if err != nil {
		return nil, err
	}
	if g.Rev != "" && g.Error == "" {
		out = append(out, ContentItem{ID: "glossary", Title: "Glossary"})
	}
	return out, nil
}

// writeSemantics stores one semantics item under finalID. The document is
// decoded again (it was validated at Open, but this is the write path), its
// name set to the final one (a rename), and written through the store:
// create-only, or against the revision just read for an overwrite.
func (p *Package) writeSemantics(ctx context.Context, t Target, it item, finalID string, overwrite bool) (string, error) {
	var opts []semantics.WriteOption
	if overwrite {
		rev := p.revFor(ctx, t, KindSemantics, finalID)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing %s: it could not be read", finalID)
		}
		opts = append(opts, semantics.WithRev(rev))
	}
	folder, name := splitSemanticsID(it.ID)
	_, finalName := splitSemanticsID(finalID)
	s := t.Semantics()
	if finalName == name {
		// Under its own name the file is written exactly as the bundle held it
		// (comments and layout kept), after the store's decoder has read it.
		_, err := s.PutSource(ctx, semanticsKind(folder), name, it.Data, opts...)
		return finalID, err
	}
	// A rename changes the name inside the document, so it is written from the
	// decoded form (comments are not kept).
	switch folder {
	case semantics.EntitiesDir:
		e, err := semantics.DecodeEntity(name, it.Data)
		if err != nil {
			return "", err
		}
		e.Name = finalName
		return finalID, s.PutEntity(ctx, e, opts...)
	case semantics.MetricsDir:
		m, err := semantics.DecodeMetric(name, it.Data)
		if err != nil {
			return "", err
		}
		m.Name = finalName
		return finalID, s.PutMetric(ctx, m, opts...)
	case semantics.JoinsDir:
		j, err := semantics.DecodeJoin(name, it.Data)
		if err != nil {
			return "", err
		}
		j.Name = finalName
		return finalID, s.PutJoin(ctx, j, opts...)
	}
	return "", fmt.Errorf("the glossary cannot be renamed")
}

func semanticsKind(folder string) semantics.Kind {
	switch folder {
	case semantics.EntitiesDir:
		return semantics.KindEntity
	case semantics.MetricsDir:
		return semantics.KindMetric
	case semantics.JoinsDir:
		return semantics.KindJoin
	}
	return semantics.KindGlossary
}

// renameSemantics finds a free id for a clashing item: "metrics/net_revenue_2".
// The glossary has one file and cannot be renamed ("").
func (r *itemImporter) renameSemantics(it item) (string, error) {
	folder, name := splitSemanticsID(it.ID)
	if folder == "" {
		return "", nil
	}
	for i := 2; i < 1000; i++ {
		suffix := fmt.Sprintf("_%d", i)
		base := name
		if len(base)+len(suffix) > 63 {
			base = base[:63-len(suffix)]
		}
		cand := folder + "/" + base + suffix
		has, err := storeHas(r.t, KindSemantics, cand)
		if err != nil {
			return "", err
		}
		if !has && !r.isTaken(KindSemantics, cand) {
			return cand, nil
		}
	}
	return "", nil
}

// noteRename records that an item of the bundle was imported under another id,
// so items that refer to it by name can follow it.
func (r *itemImporter) noteRename(it item, final string) {
	if it.Kind != KindQuery && it.Kind != KindSemantics {
		return
	}
	if r.renamed[it.Kind] == nil {
		r.renamed[it.Kind] = map[string]string{}
	}
	r.renamed[it.Kind][it.ID] = final
}

// noteSkip records that a clashing saved query or entity was not imported, so
// the metrics and joins of the same bundle that refer to it can say they now
// use the workspace's own.
func (r *itemImporter) noteSkip(it item) {
	if folder, _ := splitSemanticsID(it.ID); it.Kind != KindQuery && !(it.Kind == KindSemantics && folder == semantics.EntitiesDir) {
		return
	}
	if r.skipped == nil {
		r.skipped = map[ItemKind]map[string]bool{}
	}
	if r.skipped[it.Kind] == nil {
		r.skipped[it.Kind] = map[string]bool{}
	}
	r.skipped[it.Kind][it.ID] = true
}

// rewriteRefs points a metric at the saved query, and a join at the entities,
// that the same bundle brought in under new names. Without it a renamed
// "q" -> "q-2" would leave the imported metric using the workspace's own "q".
// Items are imported in path order, so queries/ and semantics/entities/ are
// done before the joins and metrics that refer to them. A reference to
// something the bundle did not rename is left as it is. A rewrite re-encodes
// the document (comments are not kept) and says so in the result's warnings.
func (r *itemImporter) rewriteRefs(it item) item {
	if it.Kind != KindSemantics {
		return it
	}
	folder, name := splitSemanticsID(it.ID)
	switch folder {
	case semantics.MetricsDir:
		m, err := semantics.DecodeMetric(name, it.Data)
		if err != nil {
			return it
		}
		newQ, ok := r.renamed[KindQuery][m.Query]
		if !ok {
			if r.skipped[KindQuery][m.Query] {
				r.notes = append(r.notes, fmt.Sprintf("metric %s uses the saved query %q, which was skipped because the workspace already has one by that name: it now points at the workspace's %q, which may be a different query", name, m.Query, m.Query))
			}
			return it
		}
		old := m.Query
		m.Query = newQ
		data, err := semantics.EncodeMetric(m)
		if err != nil {
			r.notes = append(r.notes, fmt.Sprintf("metric %s still uses the saved query %q, which was imported as %q: %v", name, old, newQ, err))
			return it
		}
		r.notes = append(r.notes, fmt.Sprintf("metric %s now uses the saved query %q imported with it (the workspace's own %q was kept)", name, newQ, old))
		it.Data = data
	case semantics.JoinsDir:
		j, err := semantics.DecodeJoin(name, it.Data)
		if err != nil {
			return it
		}
		changed := false
		for _, side := range []*string{&j.Left, &j.Right} {
			if r.skipped[KindSemantics][semantics.EntitiesDir+"/"+*side] {
				r.notes = append(r.notes, fmt.Sprintf("join %s uses the entity %q, which was skipped because the workspace already has one by that name: it now points at the workspace's %q, which may be a different entity", name, *side, *side))
			}
			if final, ok := r.renamed[KindSemantics][semantics.EntitiesDir+"/"+*side]; ok {
				_, newName := splitSemanticsID(final)
				r.notes = append(r.notes, fmt.Sprintf("join %s now uses the entity %q imported with it (the workspace's own %q was kept)", name, newName, *side))
				*side, changed = newName, true
			}
		}
		if !changed {
			return it
		}
		data, err := semantics.EncodeJoin(j)
		if err != nil {
			r.notes = append(r.notes, fmt.Sprintf("join %s could not be pointed at the renamed entities: %v", name, err))
			return it
		}
		it.Data = data
	}
	return it
}
