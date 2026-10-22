package artifacts

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Mock is an in-memory Store for tests of packages that depend on one. It
// keeps the two files' bytes, so revisions, conflicts and strict decoding
// behave like the file store. Set Err to make every method fail.
type Mock struct {
	mu     sync.Mutex
	byName map[string]prepared
	seen   map[string]string
	Err    error
}

var _ Store = (*Mock)(nil)

func (m *Mock) revOf(name string) string {
	p, ok := m.byName[name]
	if !ok {
		return ""
	}
	return pageRevOf(p)
}

func (m *Mock) build(name string) (*Artifact, error) {
	p := m.byName[name]
	mf, err := Decode(p.manifest)
	if err != nil {
		return nil, err
	}
	rev := pageRevOf(p)
	if m.seen == nil {
		m.seen = map[string]string{}
	}
	m.seen[name] = rev
	a := &Artifact{Name: name, Manifest: *mf, View: append([]byte(nil), p.view...), Rev: rev}
	if mf.View == ViewHTML {
		if a.Page, err = VetPage(mf, p.files); err != nil {
			return nil, err
		}
		if err = sealPage(p.manifest, p.files, a.Page); err != nil {
			return nil, err
		}
		a.View, a.Files = nil, copyFiles(p.files)
	}
	return a, nil
}

func (m *Mock) List(context.Context) ([]Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]Artifact, 0, len(m.byName))
	for name := range m.byName {
		a, err := m.build(name)
		if err != nil {
			out = append(out, Artifact{Name: name, Error: err.Error()})
			continue
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Mock) Get(_ context.Context, name string) (*Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	if _, ok := m.byName[name]; !ok {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	return m.build(name)
}

func (m *Mock) Put(_ context.Context, a *Artifact, opts ...PutOption) error {
	o := applyPutOpts(opts)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if !filestore.ValidID(a.Name) {
		return invalid("artifact name %q must be 1 to 64 lowercase letters, digits, - or _, and not a device name", a.Name)
	}
	prep, err := prepare(a)
	if err != nil {
		return err
	}
	expect := a.Rev
	if o.hasRev {
		expect = o.rev
	}
	if expect == "" && !o.overwrite && !o.hasRev {
		expect = m.seen[a.Name]
	}
	if cur := m.revOf(a.Name); !o.overwrite && expect != cur {
		return &filestore.ConflictError{Path: Dir + "/" + a.Name, Missing: cur == "", Existing: expect == "", Want: expect, Got: cur}
	}
	if m.byName == nil {
		m.byName = map[string]prepared{}
	}
	prep.view = append([]byte(nil), prep.view...)
	prep.files = copyFiles(prep.files)
	m.byName[a.Name] = prep
	a.Rev = pageRevOf(prep)
	a.Page = prep.page
	a.ManifestText = nil
	if m.seen == nil {
		m.seen = map[string]string{}
	}
	m.seen[a.Name] = a.Rev
	return nil
}

func (m *Mock) Source(_ context.Context, name string) (*Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	p, ok := m.byName[name]
	if !ok {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	rev := pageRevOf(p)
	if m.seen == nil {
		m.seen = map[string]string{}
	}
	m.seen[name] = rev
	src := &Source{Name: name, Manifest: string(p.manifest), View: string(p.view), Rev: rev, ViewFile: ViewFile}
	if mf, err := Decode(p.manifest); err == nil {
		src.ViewFile = ViewFileOf(mf)
		if mf.View == ViewHTML {
			src.Files = copyFiles(p.files)
		}
	}
	return src, nil
}

func (m *Mock) Delete(_ context.Context, name string, opts ...PutOption) error {
	o := applyPutOpts(opts)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	expect := m.seen[name]
	if o.hasRev {
		expect = o.rev
	}
	if cur := m.revOf(name); expect != "" && cur != "" && expect != cur {
		return &filestore.ConflictError{Path: Dir + "/" + name, Want: expect, Got: cur}
	}
	delete(m.byName, name)
	delete(m.seen, name)
	return nil
}

func copyFiles(files []PageFile) []PageFile {
	if files == nil {
		return nil
	}
	out := make([]PageFile, len(files))
	for i, f := range files {
		out[i] = PageFile{Path: f.Path, Data: append([]byte(nil), f.Data...)}
	}
	return out
}
