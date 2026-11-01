package semantics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/SandwichLabs/slaw/internal/connname"
)

// backend is where a core keeps file bytes: the workspace folder (FileStore)
// or a map (Mock). rel is slash-separated under the workspace root. read,
// write and remove report revisions as filestore does (the content hash) and
// satisfy its conflict and not-found errors, so the two stores behave alike.
type backend interface {
	read(rel string) (data []byte, rev string, err error)
	// write is compare-and-write: expect "" means the file must not exist.
	write(rel string, data []byte, expect string, overwrite bool) (rev string, err error)
	// remove deletes rel; expect "" deletes whatever is there.
	remove(rel, expect string, overwrite bool) error
	// names lists the files in semantics/<dir> that end in Extension, sorted.
	// Hidden and temp files are left out; a symbolic link is listed (link set)
	// so it can be shown with an error instead of silently missing.
	names(dir string) ([]fileName, error)
}

// fileName is one directory entry of a kind's folder.
type fileName struct {
	name string
	link bool
}

// TruncatedStubName (never a valid definition name, so it cannot collide with a real file) names the stub a listing ends with when its folder holds
// more than MaxFilesPerKind files: the first MaxFilesPerKind (sorted) are
// listed and this entry says how many were not. Its Error is always set.
const TruncatedStubName = "(truncated)"

// core is the whole Store over a backend.
type core struct {
	b      backend
	logger *slog.Logger
}

var _ Store = (*core)(nil)

func listKind[T any](c *core, ctx context.Context, o ops[T]) ([]T, error) {
	names, err := c.b.names(o.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list %ss: %w", o.kind, err)
	}
	more := 0
	if len(names) > MaxFilesPerKind {
		more = len(names) - MaxFilesPerKind
		names = names[:MaxFilesPerKind]
	}
	out := make([]T, 0, len(names)+1)
	for _, fn := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := fn.name
		stem := strings.TrimSuffix(n, Extension)
		if fn.link {
			out = append(out, o.stub(stem, n+" is a symbolic link; it is not read"))
			continue
		}
		if msg := stemError(stem); msg != "" {
			out = append(out, o.stub(stem, msg))
			continue
		}
		v, err := loadKind(c, o, stem)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // vanished between the listing and the read
			}
			c.logger.Warn("unreadable semantics file", "kind", o.kind, "name", stem, "error", err)
			out = append(out, o.stub(stem, errorText(err)))
			continue
		}
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool { return o.name(&out[i]) < o.name(&out[j]) })
	if more > 0 {
		out = append(out, o.stub(TruncatedStubName, fmt.Sprintf("%d more files in %s/%s are not listed (limit %d)", more, Dir, o.dir, MaxFilesPerKind)))
	}
	return out, nil
}

// errorText is an error's message without the "invalid semantics definition:"
// prefix, which every listed Error would otherwise repeat.
func errorText(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
}

// ReadableError is a store error as one sentence: the store wraps a
// decoder's refusal as `failed to get metric "x": invalid semantics
// definition: <file>: <why>`, and the first two layers say nothing the caller
// does not already know.
func ReadableError(err error) string {
	s := err.Error()
	if strings.HasPrefix(s, "failed to get ") {
		if i := strings.Index(s, `": `); i >= 0 {
			s = s[i+3:]
		}
	}
	return strings.TrimPrefix(s, ErrInvalid.Error()+": ")
}

func loadKind[T any](c *core, o ops[T], name string) (*T, error) {
	data, rev, err := c.b.read(o.rel(name))
	if err != nil {
		return nil, err
	}
	v, err := o.decode(name, data)
	if err != nil {
		return nil, err
	}
	o.meta(v).Rev = rev
	return v, nil
}

func getKind[T any](c *core, o ops[T], name string) (*T, error) {
	if !connname.Valid(name) {
		return nil, fmt.Errorf("failed to get %s %q: %w", o.kind, name, invalid("not a valid name"))
	}
	v, err := loadKind(c, o, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &notFoundError{kind: o.kind, name: name}
		}
		return nil, fmt.Errorf("failed to get %s %q: %w", o.kind, name, err)
	}
	return v, nil
}

// caseVariantError says a differently-cased file would be the same file on a
// case-insensitive filesystem, so a write is refused rather than risk it.
type caseVariantError struct{ want, have string }

func (e *caseVariantError) Error() string {
	return fmt.Sprintf("%s exists with different capitalisation (%s); on a case-insensitive filesystem a write would replace it. Rename or remove it first", e.have, e.want)
}
func (e *caseVariantError) Is(target error) bool { return target == ErrConflict }

// checkCaseVariant refuses a write when the folder holds a file whose name
// equals the target's ignoring case but not exactly.
func (c *core) checkCaseVariant(dir, name string) error {
	names, err := c.b.names(dir)
	if err != nil {
		return err
	}
	want := name + Extension
	for _, fn := range names {
		if fn.name != want && strings.EqualFold(fn.name, want) {
			return &caseVariantError{want: want, have: fn.name}
		}
	}
	return nil
}

func putKind[T any](c *core, o ops[T], v *T, opts []WriteOption) error {
	w := applyWriteOpts(opts)
	data, err := o.encode(v)
	if err != nil {
		return err
	}
	name := o.name(v)
	if err := c.checkCaseVariant(o.dir, name); err != nil {
		return fmt.Errorf("failed to write %s %q: %w", o.kind, name, err)
	}
	rel := o.rel(name)
	// A file that exists but cannot be read is never replaced unless the
	// caller says so: the store would otherwise drop what it could not parse.
	if !w.overwrite {
		if cur, _, rerr := c.b.read(rel); rerr == nil {
			if _, derr := o.decode(name, cur); derr != nil {
				return fmt.Errorf("failed to write %s %q: not saved, because that would replace a file that could not be read (%s); fix the file first or overwrite it", o.kind, name, errorText(derr))
			}
		}
	}
	rev, err := c.b.write(rel, data, w.expect(o.meta(v).Rev), w.overwrite)
	if err != nil {
		return fmt.Errorf("failed to write %s %q: %w", o.kind, name, err)
	}
	o.meta(v).Rev, o.meta(v).Error = rev, ""
	return nil
}

func deleteKind[T any](c *core, o ops[T], name string, opts []WriteOption) error {
	w := applyWriteOpts(opts)
	if !connname.Valid(name) {
		return fmt.Errorf("failed to delete %s %q: %w", o.kind, name, invalid("not a valid name"))
	}
	if _, _, err := c.b.read(o.rel(name)); err != nil && errors.Is(err, ErrNotFound) {
		return &notFoundError{kind: o.kind, name: name}
	}
	if err := c.b.remove(o.rel(name), w.rev, w.overwrite); err != nil {
		return fmt.Errorf("failed to delete %s %q: %w", o.kind, name, err)
	}
	return nil
}

// Entities implements Store.
func (c *core) Entities(ctx context.Context) ([]Entity, error) { return listKind(c, ctx, entityOps) }

// Metrics implements Store.
func (c *core) Metrics(ctx context.Context) ([]Metric, error) { return listKind(c, ctx, metricOps) }

// Joins implements Store.
func (c *core) Joins(ctx context.Context) ([]Join, error) { return listKind(c, ctx, joinOps) }

// Entity implements Store.
func (c *core) Entity(_ context.Context, name string) (*Entity, error) {
	return getKind(c, entityOps, name)
}

// Metric implements Store.
func (c *core) Metric(_ context.Context, name string) (*Metric, error) {
	return getKind(c, metricOps, name)
}

// Join implements Store.
func (c *core) Join(_ context.Context, name string) (*Join, error) {
	return getKind(c, joinOps, name)
}

// PutEntity implements Store.
func (c *core) PutEntity(_ context.Context, e *Entity, opts ...WriteOption) error {
	return putKind(c, entityOps, e, opts)
}

// PutMetric implements Store.
func (c *core) PutMetric(_ context.Context, m *Metric, opts ...WriteOption) error {
	return putKind(c, metricOps, m, opts)
}

// PutJoin implements Store.
func (c *core) PutJoin(_ context.Context, j *Join, opts ...WriteOption) error {
	return putKind(c, joinOps, j, opts)
}

// DeleteEntity implements Store.
func (c *core) DeleteEntity(_ context.Context, name string, opts ...WriteOption) error {
	return deleteKind(c, entityOps, name, opts)
}

// DeleteMetric implements Store.
func (c *core) DeleteMetric(_ context.Context, name string, opts ...WriteOption) error {
	return deleteKind(c, metricOps, name, opts)
}

// DeleteJoin implements Store.
func (c *core) DeleteJoin(_ context.Context, name string, opts ...WriteOption) error {
	return deleteKind(c, joinOps, name, opts)
}

const glossaryRel = Dir + "/" + GlossaryFile

// Glossary implements Store. A glossary that cannot be read (too large, a
// control character) comes back with Error set and no text, like a listed
// definition, so it can be found and fixed.
func (c *core) Glossary(_ context.Context) (*Glossary, error) {
	data, rev, err := c.b.read(glossaryRel)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &Glossary{Terms: []string{}}, nil
		}
		return nil, fmt.Errorf("failed to read the glossary: %w", err)
	}
	g, err := DecodeGlossary(data)
	if err != nil {
		return &Glossary{Terms: []string{}, Meta: Meta{Rev: rev, Error: errorText(err)}}, nil
	}
	g.Rev = rev
	return g, nil
}

// PutGlossary implements Store.
func (c *core) PutGlossary(_ context.Context, g *Glossary, opts ...WriteOption) error {
	w := applyWriteOpts(opts)
	if _, err := DecodeGlossary([]byte(g.Text)); err != nil {
		return err
	}
	if !w.overwrite {
		if cur, _, rerr := c.b.read(glossaryRel); rerr == nil {
			if _, derr := DecodeGlossary(cur); derr != nil {
				return fmt.Errorf("failed to write the glossary: not saved, because that would replace a file that could not be read (%s); fix the file first or overwrite it", errorText(derr))
			}
		}
	}
	rev, err := c.b.write(glossaryRel, []byte(g.Text), w.expect(g.Rev), w.overwrite)
	if err != nil {
		return fmt.Errorf("failed to write the glossary: %w", err)
	}
	g.Rev, g.Error = rev, ""
	return nil
}

// PutSource implements Store.
func (c *core) PutSource(_ context.Context, kind Kind, name string, data []byte, opts ...WriteOption) (string, error) {
	w := applyWriteOpts(opts)
	var rel string
	var decode func(b []byte) error
	switch kind {
	case KindEntity:
		rel, decode = entityOps.rel(name), func(b []byte) error { _, err := DecodeEntity(name, b); return err }
	case KindMetric:
		rel, decode = metricOps.rel(name), func(b []byte) error { _, err := DecodeMetric(name, b); return err }
	case KindJoin:
		rel, decode = joinOps.rel(name), func(b []byte) error { _, err := DecodeJoin(name, b); return err }
	case KindGlossary:
		rel, decode = glossaryRel, func(b []byte) error { _, err := DecodeGlossary(b); return err }
	default:
		return "", invalid("unknown kind %q", kind)
	}
	if kind != KindGlossary && !connname.Valid(name) {
		return "", invalid("%s name %q is not valid", kind, name)
	}
	if err := decode(data); err != nil {
		return "", err
	}
	if kind != KindGlossary {
		dir := map[Kind]string{KindEntity: EntitiesDir, KindMetric: MetricsDir, KindJoin: JoinsDir}[kind]
		if err := c.checkCaseVariant(dir, name); err != nil {
			return "", fmt.Errorf("failed to write %s %q: %w", kind, name, err)
		}
	}
	if !w.overwrite {
		if cur, _, rerr := c.b.read(rel); rerr == nil {
			if derr := decode(cur); derr != nil {
				return "", fmt.Errorf("failed to write %s %q: not saved, because that would replace a file that could not be read (%s); fix the file first or overwrite it", kind, name, errorText(derr))
			}
		}
	}
	rev, err := c.b.write(rel, data, w.expect(""), w.overwrite)
	if err != nil {
		return "", fmt.Errorf("failed to write %s %q: %w", kind, name, err)
	}
	return rev, nil
}

// Source implements Store.
func (c *core) Source(_ context.Context, kind Kind, name string) (*Source, error) {
	var rel string
	switch kind {
	case KindEntity:
		rel = entityOps.rel(name)
	case KindMetric:
		rel = metricOps.rel(name)
	case KindJoin:
		rel = joinOps.rel(name)
	case KindGlossary:
		rel, name = glossaryRel, GlossaryFile
	default:
		return nil, invalid("unknown kind %q", kind)
	}
	if kind != KindGlossary && !connname.Valid(name) {
		return nil, invalid("%s name %q is not valid", kind, name)
	}
	data, rev, err := c.b.read(rel)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &notFoundError{kind: kind, name: name}
		}
		return nil, fmt.Errorf("failed to read %s %q: %w", kind, name, err)
	}
	return &Source{Kind: kind, Name: name, Text: string(data), Rev: rev}, nil
}
