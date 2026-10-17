package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// FileStore keeps artifacts as artifacts/<name>/artifact.yaml and
// artifacts/<name>/view.a2ui.json under a workspace root.
//
// The folder name is the artifact name and must pass filestore.ValidID
// (lowercase, no "..", no device names). A folder or file that is a symbolic
// link is refused by Get and Put and listed with an error by List (a link
// could lead out of the workspace); a symlinked folder is not even listed
// (filestore.ListSubdirs skips it). Files beside the two are left alone.
//
// # One revision for two files
//
// Rev hashes both files (see Rev). Put reads both files and checks the
// caller's revision against the pair before writing either; it then writes the
// view and then the manifest, each with filestore.WriteAtomic and a per-file
// compare-and-write on the bytes it just read, so a writer that slips in
// between is caught on that file. A crash between the two writes leaves a pair
// that fails validation (or a new view beside the old manifest): it lists with
// its error and never runs half-checked. A Put that is refused after the view
// was written puts the old view back.
type FileStore struct {
	root string
	obs  filestore.Observer

	wmu  sync.Mutex // serialises Put and Delete in this process
	mu   sync.Mutex
	seen map[string]string
}

// FileStoreOption configures NewFileStore.
type FileStoreOption func(*FileStore)

// WithObserver reports this store's own writes so a watcher can ignore them.
func WithObserver(o filestore.Observer) FileStoreOption {
	return func(s *FileStore) { s.obs = o }
}

// NewFileStore returns the file-backed Store rooted at a workspace folder.
func NewFileStore(root string, opts ...FileStoreOption) *FileStore {
	s := &FileStore{root: root, seen: map[string]string{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

var _ Store = (*FileStore)(nil)

func rel(name, file string) string { return Dir + "/" + name + "/" + file }

func (s *FileStore) remember(name, rev string) {
	s.mu.Lock()
	s.seen[name] = rev
	s.mu.Unlock()
}

func (s *FileStore) lastSeen(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[name]
}

// checkFolder refuses an artifacts folder or an artifact folder that is a
// symbolic link or not a folder. A folder that does not exist is fine.
func (s *FileStore) checkFolder(name string) error {
	for _, r := range []string{Dir, Dir + "/" + name} {
		fi, err := os.Lstat(filepath.Join(s.root, filepath.FromSlash(r)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to inspect %s: %w", r, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return invalid("%s is a symbolic link", r)
		}
		if !fi.IsDir() {
			return invalid("%s is not a folder", r)
		}
	}
	return nil
}

type readParts struct {
	manifest, view filePart
	modTime        time.Time
}

// read loads both files of an artifact. A file that does not exist is an
// absent part; one that is a symbolic link, not a regular file or too large is
// an error.
func (s *FileStore) read(name string) (readParts, error) {
	if err := s.checkFolder(name); err != nil {
		return readParts{}, err
	}
	var out readParts
	for _, f := range []struct {
		file string
		part *filePart
	}{{ManifestFile, &out.manifest}, {ViewFile, &out.view}} {
		data, st, err := filestore.Read(s.root, rel(name, f.file))
		if errors.Is(err, filestore.ErrNotFound) {
			continue
		}
		if err != nil {
			return readParts{}, err
		}
		*f.part = filePart{data: data, present: true}
		if st.ModTime.After(out.modTime) {
			out.modTime = st.ModTime
		}
	}
	return out, nil
}

func (s *FileStore) load(name string) (*Artifact, error) {
	p, err := s.read(name)
	if err != nil {
		return nil, err
	}
	if !p.manifest.present && !p.view.present {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	if !p.manifest.present {
		return nil, invalid("%s is missing", ManifestFile)
	}
	m, err := Decode(p.manifest.data)
	if err != nil {
		return nil, err
	}
	if !p.view.present {
		return nil, invalid("%s is missing", ViewFile)
	}
	if err := ValidateView(m, p.view.data); err != nil {
		return nil, err
	}
	return &Artifact{
		Name: name, Manifest: *m, View: p.view.data,
		Rev: revision(p.manifest, p.view), UpdatedAt: p.modTime.UTC().Format(time.RFC3339),
	}, nil
}

// List implements Store.
func (s *FileStore) List(ctx context.Context) ([]Artifact, error) {
	dirs, err := filestore.ListSubdirs(s.root, Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}
	out := []Artifact{}
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filestore.ValidID(d) {
			out = append(out, Artifact{Name: d, Error: fmt.Sprintf("%q is not a usable artifact name: use lowercase letters, digits, - and _ (at most 64)", d)})
			continue
		}
		a, err := s.load(d)
		if err != nil {
			out = append(out, Artifact{Name: d, Error: err.Error()})
			continue
		}
		s.remember(d, a.Rev)
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get implements Store.
func (s *FileStore) Get(_ context.Context, name string) (*Artifact, error) {
	if !filestore.ValidID(name) {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	a, err := s.load(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact %q: %w", name, err)
	}
	s.remember(name, a.Rev)
	return a, nil
}

// Put implements Store.
func (s *FileStore) Put(_ context.Context, a *Artifact, opts ...PutOption) error {
	o := applyPutOpts(opts)
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
		expect = s.lastSeen(a.Name)
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()
	cur, err := s.read(a.Name)
	if err != nil {
		return fmt.Errorf("failed to save artifact %q: %w", a.Name, err)
	}
	curRev := revision(cur.manifest, cur.view)
	if !o.overwrite && expect != curRev {
		return &filestore.ConflictError{
			Path: Dir + "/" + a.Name, Missing: curRev == "", Existing: expect == "", Want: expect, Got: curRev,
		}
	}
	return s.write(a, prep, cur)
}

// write stores the view, then the manifest. The manifest is re-read just
// before the view is written (a writer that touched only artifact.yaml since
// the pair check is a conflict before anything changes), and each write is a
// compare-and-write against the bytes just read. If the manifest write still
// fails, the view is put back, so a refused Put leaves the pair as it found it
// (best effort: a crash between the two writes leaves a new view beside the old
// manifest, which lists with its error and never runs half-checked).
func (s *FileStore) write(a *Artifact, prep prepared, cur readParts) error {
	if err := s.manifestUnchanged(a.Name, cur.manifest); err != nil {
		return err
	}
	viewHash := filestore.HashBytes(prep.view)
	st, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(a.Name, ViewFile), Data: prep.view, Expect: expectOf(cur.view)}, s.obs)
	if err != nil {
		return fmt.Errorf("failed to save artifact %q: %w", a.Name, err)
	}
	newest := st.ModTime
	mst, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(a.Name, ManifestFile), Data: prep.manifest, Expect: expectOf(cur.manifest)}, s.obs)
	if err != nil {
		s.restoreView(a.Name, cur.view, viewHash)
		return fmt.Errorf("failed to save artifact %q: %w", a.Name, err)
	}
	if mst.ModTime.After(newest) {
		newest = mst.ModTime
	}
	a.Rev = RevOf(prep.manifest, prep.view)
	a.ManifestText = nil // consumed: a reused value must encode its Manifest next time
	a.UpdatedAt = newest.UTC().Format(time.RFC3339)
	s.remember(a.Name, a.Rev)
	return nil
}

func expectOf(p filePart) string {
	if !p.present {
		return ""
	}
	return filestore.HashBytes(p.data)
}

// manifestUnchanged reports a conflict when artifact.yaml is no longer what the
// pair check read.
func (s *FileStore) manifestUnchanged(name string, was filePart) error {
	now, err := s.read(name)
	if err != nil {
		return fmt.Errorf("failed to save artifact %q: %w", name, err)
	}
	if now.manifest.present != was.present || expectOf(now.manifest) != expectOf(was) {
		return fmt.Errorf("failed to save artifact %q: %w", name,
			&filestore.ConflictError{Path: rel(name, ManifestFile), Want: expectOf(was), Got: expectOf(now.manifest), Missing: !now.manifest.present})
	}
	return nil
}

// restoreView puts the view the write replaced back, only if it is still the
// bytes this Put wrote (written is their hash). Errors are ignored: the
// original failure is the one worth reporting.
func (s *FileStore) restoreView(name string, was filePart, written string) {
	if !was.present {
		_ = filestore.Remove(s.root, rel(name, ViewFile), written, false, s.obs)
		return
	}
	_, _ = filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(name, ViewFile), Data: was.data, Expect: written}, s.obs)
}

// Source implements Store.
func (s *FileStore) Source(_ context.Context, name string) (*Source, error) {
	if !filestore.ValidID(name) {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	p, err := s.read(name)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact %q: %w", name, err)
	}
	if !p.manifest.present && !p.view.present {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	rev := revision(p.manifest, p.view)
	s.remember(name, rev)
	return &Source{
		Name: name, Manifest: string(p.manifest.data), View: string(p.view.data),
		ManifestMissing: !p.manifest.present, ViewMissing: !p.view.present,
		Rev: rev, UpdatedAt: p.modTime.UTC().Format(time.RFC3339),
	}, nil
}

// Delete implements Store. It refuses (with ErrConflict) when the artifact
// changed on disk since the revision the caller loaded (WithRev) or, without
// one, since this store last read it, like the skills and queries stores. It
// removes both files, then the folder when that leaves it empty (other files
// beside them are never touched).
func (s *FileStore) Delete(_ context.Context, name string, opts ...PutOption) error {
	o := applyPutOpts(opts)
	if !filestore.ValidID(name) {
		return nil
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	cur, err := s.read(name)
	if err != nil {
		return fmt.Errorf("failed to delete artifact %q: %w", name, err)
	}
	expect := s.lastSeen(name)
	if o.hasRev {
		expect = o.rev
	}
	if now := revision(cur.manifest, cur.view); expect != "" && now != "" && expect != now {
		return fmt.Errorf("failed to delete artifact %q: %w", name, &filestore.ConflictError{Path: Dir + "/" + name, Want: expect, Got: now})
	}
	// Each file goes only if it is still the bytes just read, so a writer in
	// another process (this lock covers only this one) that lands after the
	// pair check is a conflict, not a deletion. A conflict on the second file
	// leaves a half-deleted pair, which lists with its error.
	for _, f := range []struct {
		file string
		part filePart
	}{{ViewFile, cur.view}, {ManifestFile, cur.manifest}} {
		if err := filestore.Remove(s.root, rel(name, f.file), expectOf(f.part), false, s.obs); err != nil {
			return fmt.Errorf("failed to delete artifact %q: %w", name, err)
		}
	}
	filestore.RemoveEmptyDir(s.root, Dir+"/"+name)
	s.mu.Lock()
	delete(s.seen, name)
	s.mu.Unlock()
	return nil
}
