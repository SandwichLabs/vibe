package semantics

import "context"

// Store is where a workspace keeps its semantic definitions: three folders of
// one yaml file each and one glossary. The FileStore keeps them under
// semantics/; the Mock keeps them in memory for tests of packages that depend
// on one.
//
// Every item carries Rev, the content hash of its one file as loaded. A write
// compares it against the file: a stale one is ErrConflict and changes nothing.
// A file that cannot be read still lists, under its file stem, with Error set;
// Get refuses it (ErrInvalid) and a write over it needs Overwrite.
type Store interface {
	// Entities, Metrics and Joins list every definition ordered by name; never
	// nil. An unreadable file is listed with Error set, never hidden. More
	// than MaxFilesPerKind files is an error.
	Entities(ctx context.Context) ([]Entity, error)
	Metrics(ctx context.Context) ([]Metric, error)
	Joins(ctx context.Context) ([]Join, error)

	// Entity, Metric and Join return one definition. A missing name satisfies
	// errors.Is(err, ErrNotFound); an unreadable or invalid file is an error
	// wrapping ErrInvalid.
	Entity(ctx context.Context, name string) (*Entity, error)
	Metric(ctx context.Context, name string) (*Metric, error)
	Join(ctx context.Context, name string) (*Join, error)

	// PutEntity, PutMetric and PutJoin validate and write a definition. With
	// neither a revision (the item's Rev or WithRev) nor Overwrite the write
	// only creates: an existing file is ErrConflict. On success the item's Rev
	// is updated.
	PutEntity(ctx context.Context, e *Entity, opts ...WriteOption) error
	PutMetric(ctx context.Context, m *Metric, opts ...WriteOption) error
	PutJoin(ctx context.Context, j *Join, opts ...WriteOption) error

	// DeleteEntity, DeleteMetric and DeleteJoin remove a definition; a missing
	// name is ErrNotFound. With WithRev a file that changed since is
	// ErrConflict; without one it deletes whatever is there.
	DeleteEntity(ctx context.Context, name string, opts ...WriteOption) error
	DeleteMetric(ctx context.Context, name string, opts ...WriteOption) error
	DeleteJoin(ctx context.Context, name string, opts ...WriteOption) error

	// Source returns a file's text exactly as stored, valid or not, with its
	// revision: what an editor or `vibe semantics show` displays, so a broken
	// file can be read and fixed. name is ignored for KindGlossary. A missing
	// file is ErrNotFound.
	Source(ctx context.Context, kind Kind, name string) (*Source, error)

	// PutSource validates file bytes with the kind's strict decoder and writes
	// them exactly as given (comments and layout kept), under the same
	// revision rules as the Put methods. It is how a bundle import keeps a
	// hand-written file byte for byte. name is ignored for KindGlossary. It
	// returns the new revision.
	PutSource(ctx context.Context, kind Kind, name string, data []byte, opts ...WriteOption) (rev string, err error)

	// Glossary returns glossary.md; a missing file is an empty Glossary with
	// no Rev, not an error. PutGlossary validates and writes it under the same
	// revision rules as the others.
	Glossary(ctx context.Context) (*Glossary, error)
	PutGlossary(ctx context.Context, g *Glossary, opts ...WriteOption) error
}

// Source is one file's text as stored.
type Source struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
	Text string `json:"text"`
	Rev  string `json:"rev"`
}

// WriteOption tunes one write.
type WriteOption func(*writeOpts)

type writeOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
}

// WithOverwrite skips the conflict check ("overwrite" in the conflict banner), and
// is the only way to write over a file that could not be read.
func WithOverwrite() WriteOption { return func(o *writeOpts) { o.overwrite = true } }

// WithRev sets the revision the caller loaded, overriding the item's Rev.
// WithRev("") says the caller loaded nothing: the write only creates.
func WithRev(rev string) WriteOption {
	return func(o *writeOpts) { o.rev, o.hasRev = rev, true }
}

func applyWriteOpts(opts []WriteOption) writeOpts {
	var o writeOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

// expect is the revision a write compares against.
func (o writeOpts) expect(itemRev string) string {
	if o.hasRev {
		return o.rev
	}
	return itemRev
}

// ops describes one kind of yaml definition to the shared store code, so the
// FileStore and the Mock each have one implementation of list, get, put and
// delete instead of three.
type ops[T any] struct {
	kind   Kind
	dir    string // EntitiesDir, MetricsDir or JoinsDir
	decode func(stem string, data []byte) (*T, error)
	encode func(*T) ([]byte, error)
	name   func(*T) string
	meta   func(*T) *Meta
	stub   func(stem, msg string) T
}

var entityOps = ops[Entity]{
	kind: KindEntity, dir: EntitiesDir, decode: DecodeEntity, encode: EncodeEntity,
	name: func(e *Entity) string { return e.Name },
	meta: func(e *Entity) *Meta { return &e.Meta },
	stub: func(stem, msg string) Entity { return Entity{Name: stem, Meta: Meta{Error: msg}} },
}

var metricOps = ops[Metric]{
	kind: KindMetric, dir: MetricsDir, decode: DecodeMetric, encode: EncodeMetric,
	name: func(m *Metric) string { return m.Name },
	meta: func(m *Metric) *Meta { return &m.Meta },
	stub: func(stem, msg string) Metric { return Metric{Name: stem, Meta: Meta{Error: msg}} },
}

var joinOps = ops[Join]{
	kind: KindJoin, dir: JoinsDir, decode: DecodeJoin, encode: EncodeJoin,
	name: func(j *Join) string { return j.Name },
	meta: func(j *Join) *Meta { return &j.Meta },
	stub: func(stem, msg string) Join { return Join{Name: stem, Meta: Meta{Error: msg}} },
}

func (o ops[T]) rel(name string) string { return Dir + "/" + o.dir + "/" + name + Extension }

// stemError says why a file stem cannot be a definition name, or "".
func stemError(stem string) string {
	if err := validIdent("name", stem); err != nil {
		return "the file name is not a valid definition name: use lower-case letters, digits and _"
	}
	return ""
}
