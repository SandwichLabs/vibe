package semantics

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

// Mock is an in-memory Store for tests of packages that depend on one. It
// keeps file bytes, so strict decoding, revisions, conflicts and the
// unreadable-file rules behave like the FileStore. Set Err to make every
// method fail. The zero value is ready to use.
type Mock struct {
	mu    sync.Mutex
	once  sync.Once
	c     *core
	files map[string][]byte
	Err   error
}

var _ Store = (*Mock)(nil)

// SetFile puts raw bytes at a path under semantics/ (for example
// "entities/customer.yaml"), valid or not, as a test for an unreadable file
// would write them to disk.
func (m *Mock) SetFile(rel string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[Dir+"/"+rel] = append([]byte(nil), data...)
}

func (m *Mock) core() *core {
	m.once.Do(func() { m.c = &core{b: m, logger: slog.New(slog.DiscardHandler)} })
	return m.c
}

func (m *Mock) read(rel string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, "", m.Err
	}
	data, ok := m.files[rel]
	if !ok {
		return nil, "", fmt.Errorf("%s: %w", rel, filestore.ErrNotFound)
	}
	return append([]byte(nil), data...), filestore.HashBytes(data), nil
}

func (m *Mock) write(rel string, data []byte, expect string, overwrite bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return "", m.Err
	}
	cur, exists := m.files[rel]
	curRev := ""
	if exists {
		curRev = filestore.HashBytes(cur)
	}
	if !overwrite {
		switch {
		case expect == "" && exists:
			return "", &filestore.ConflictError{Path: rel, Existing: true, Got: curRev}
		case expect != "" && !exists:
			return "", &filestore.ConflictError{Path: rel, Missing: true, Want: expect}
		case expect != "" && curRev != expect:
			return "", &filestore.ConflictError{Path: rel, Want: expect, Got: curRev}
		}
	}
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[rel] = append([]byte(nil), data...)
	return filestore.HashBytes(data), nil
}

func (m *Mock) remove(rel, expect string, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	cur, exists := m.files[rel]
	if !exists {
		return nil
	}
	if curRev := filestore.HashBytes(cur); !overwrite && expect != "" && curRev != expect {
		return &filestore.ConflictError{Path: rel, Want: expect, Got: curRev}
	}
	delete(m.files, rel)
	return nil
}

func (m *Mock) names(dir string) ([]fileName, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	prefix := Dir + "/" + dir + "/"
	var out []fileName
	for rel := range m.files {
		if n, ok := strings.CutPrefix(rel, prefix); ok && !strings.Contains(n, "/") && strings.HasSuffix(n, Extension) && !filestore.IsTemp(n) {
			out = append(out, fileName{name: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func (m *Mock) Entities(ctx context.Context) ([]Entity, error) { return m.core().Entities(ctx) }
func (m *Mock) Metrics(ctx context.Context) ([]Metric, error)  { return m.core().Metrics(ctx) }
func (m *Mock) Joins(ctx context.Context) ([]Join, error)      { return m.core().Joins(ctx) }

func (m *Mock) Entity(ctx context.Context, name string) (*Entity, error) {
	return m.core().Entity(ctx, name)
}

func (m *Mock) Metric(ctx context.Context, name string) (*Metric, error) {
	return m.core().Metric(ctx, name)
}

func (m *Mock) Join(ctx context.Context, name string) (*Join, error) {
	return m.core().Join(ctx, name)
}

func (m *Mock) PutEntity(ctx context.Context, e *Entity, opts ...WriteOption) error {
	return m.core().PutEntity(ctx, e, opts...)
}

func (m *Mock) PutMetric(ctx context.Context, mt *Metric, opts ...WriteOption) error {
	return m.core().PutMetric(ctx, mt, opts...)
}

func (m *Mock) PutJoin(ctx context.Context, j *Join, opts ...WriteOption) error {
	return m.core().PutJoin(ctx, j, opts...)
}

func (m *Mock) DeleteEntity(ctx context.Context, name string, opts ...WriteOption) error {
	return m.core().DeleteEntity(ctx, name, opts...)
}

func (m *Mock) DeleteMetric(ctx context.Context, name string, opts ...WriteOption) error {
	return m.core().DeleteMetric(ctx, name, opts...)
}

func (m *Mock) DeleteJoin(ctx context.Context, name string, opts ...WriteOption) error {
	return m.core().DeleteJoin(ctx, name, opts...)
}

func (m *Mock) Glossary(ctx context.Context) (*Glossary, error) { return m.core().Glossary(ctx) }

func (m *Mock) PutGlossary(ctx context.Context, g *Glossary, opts ...WriteOption) error {
	return m.core().PutGlossary(ctx, g, opts...)
}

func (m *Mock) PutSource(ctx context.Context, kind Kind, name string, data []byte, opts ...WriteOption) (string, error) {
	return m.core().PutSource(ctx, kind, name, data, opts...)
}

func (m *Mock) Source(ctx context.Context, kind Kind, name string) (*Source, error) {
	return m.core().Source(ctx, kind, name)
}
