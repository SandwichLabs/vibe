package artifacts

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

// maxPageDirDepth bounds the walk of html/: html/assets/<folder>/<file> is the
// deepest layout H1 allows, and a deeper tree is refused when the walk reaches
// it rather than followed.
const maxPageDirDepth = 3

// readPageFiles reads the file set of artifacts/<name>/html/. It returns nil
// files for a folder that does not exist. It refuses (never repairs) a
// symbolic link, a file that is not a regular file, a file over the per-file
// cap (checked before it is read), more files than the cap, and a tree deeper
// than the layout allows; the rules about names, extensions and contents are
// applied by VetPage on what was read. Temporary files left by an atomic write
// (filestore.TempPrefix) are skipped; any other dotfile is read and then
// refused by the vet. The newest modification time is returned for UpdatedAt.
func readPageFiles(root, name string) ([]PageFile, time.Time, error) {
	dir := rel(name, HTMLDir)
	abs, err := filestore.Resolve(root, dir)
	if err != nil {
		return nil, time.Time{}, invalid("%s: %v", dir, err)
	}
	fi, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to inspect %s: %w", dir, err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return nil, time.Time{}, invalid("%s is a symbolic link or not a folder", dir)
	}
	w := &pageWalk{root: root, name: name, files: []PageFile{}}
	if err := w.dir("", 0); err != nil {
		return nil, time.Time{}, err
	}
	sort.Slice(w.files, func(i, j int) bool { return w.files[i].Path < w.files[j].Path })
	return w.files, w.newest, nil
}

type pageWalk struct {
	root, name string
	files      []PageFile
	newest     time.Time
}

func (w *pageWalk) dir(sub string, depth int) error {
	rdir := path.Join(rel(w.name, HTMLDir), sub)
	abs, err := filestore.Resolve(w.root, rdir)
	if err != nil {
		return invalid("%s: %v", rdir, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", rdir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filestore.TempPrefix) || IgnoredPageFile(e.Name()) {
			continue // an interrupted atomic write, or OS clutter; other dotfiles are refused by the vet
		}
		p := path.Join(sub, e.Name())
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			return invalid("%s/%s is a symbolic link", HTMLDir, p)
		case e.IsDir():
			if depth+1 > maxPageDirDepth-1 {
				return invalid("%s/%s is nested too deep", HTMLDir, p)
			}
			if err := w.dir(p, depth+1); err != nil {
				return err
			}
		case e.Type().IsRegular():
			if err := w.file(p, e); err != nil {
				return err
			}
		default:
			return invalid("%s/%s is not a regular file", HTMLDir, p)
		}
	}
	return nil
}

func (w *pageWalk) file(p string, e fs.DirEntry) error {
	if len(w.files) >= MaxPageFiles {
		return invalid("more than %d files under %s/", MaxPageFiles, HTMLDir)
	}
	info, err := e.Info()
	if err != nil {
		return fmt.Errorf("failed to inspect %s/%s: %w", HTMLDir, p, err)
	}
	limit := int64(MaxPageFileBytes)
	if path.Ext(p) == ".tmpl" {
		limit = MaxTemplateBytes
	}
	if info.Size() > limit {
		return invalid("%s/%s is %d bytes, over the %d byte limit", HTMLDir, p, info.Size(), limit)
	}
	data, modTime, err := readPageFile(w.root, rel(w.name, HTMLDir+"/"+p), info, limit)
	if err != nil {
		return invalid("%s/%s: %v", HTMLDir, p, err)
	}
	if modTime.After(w.newest) {
		w.newest = modTime
	}
	w.files = append(w.files, PageFile{Path: p, Data: data})
	return nil
}

func pageFileRel(name, p string) string { return rel(name, HTMLDir+"/"+p) }

func fileHashes(files []PageFile) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = filestore.HashBytes(f.Data)
	}
	return m
}

// writeHTML stores a view: html artifact: the page files, then the manifest,
// then the removal of what the new set no longer holds (and of the old view
// kind's file). Each file is a compare-and-write against the bytes read at the
// pair check; a file whose bytes are unchanged is not rewritten. A refused
// manifest write puts the files back (best effort, as for the other kinds).
func (s *FileStore) writeHTML(a *Artifact, prep prepared, cur readParts) error {
	have := fileHashes(cur.pageFiles())
	var wrote []PageFile
	var newest time.Time
	fail := func(err error) error {
		s.restorePageFiles(a.Name, wrote, cur.pageFiles())
		return fmt.Errorf("failed to save artifact %q: %w", a.Name, err)
	}
	for _, f := range prep.files {
		h := filestore.HashBytes(f.Data)
		if have[f.Path] == h {
			continue
		}
		st, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: pageFileRel(a.Name, f.Path), Data: f.Data, Expect: have[f.Path]}, s.obs)
		if err != nil {
			return fail(err)
		}
		wrote = append(wrote, f)
		if st.ModTime.After(newest) {
			newest = st.ModTime
		}
	}
	mst, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(a.Name, ManifestFile), Data: prep.manifest, Expect: expectOf(cur.manifest)}, s.obs)
	if err != nil {
		return fail(err)
	}
	if mst.ModTime.After(newest) {
		newest = mst.ModTime
	}
	if err := s.removeStale(a.Name, cur, prep.files); err != nil {
		return err
	}
	a.Rev = pageRevOf(prep)
	a.Files, a.Page, a.View = copyFiles(prep.files), prep.page, nil
	a.ManifestText = nil
	a.UpdatedAt = newest.UTC().Format(time.RFC3339)
	s.remember(a.Name, a.Rev)
	return nil
}

// removeStale removes, after a successful write, the old view file of a
// changed kind and every page file the new set no longer holds.
func (s *FileStore) removeStale(name string, cur readParts, keep []PageFile) error {
	if cur.view.present {
		if err := filestore.Remove(s.root, rel(name, cur.viewFile), expectOf(cur.view), false, s.obs); err != nil {
			return fmt.Errorf("failed to remove the old view file of artifact %q: %w", name, err)
		}
	}
	kept := map[string]bool{}
	for _, f := range keep {
		kept[f.Path] = true
	}
	var gone []PageFile
	for _, f := range cur.pageFiles() {
		if !kept[f.Path] {
			gone = append(gone, f)
		}
	}
	return s.removePageFiles(name, gone)
}

// removePageFiles removes files, each only if still the bytes that were read,
// then the folders that leave empty.
func (s *FileStore) removePageFiles(name string, files []PageFile) error {
	if len(files) == 0 {
		return nil
	}
	dirs := map[string]bool{}
	for _, f := range files {
		if err := filestore.Remove(s.root, pageFileRel(name, f.Path), filestore.HashBytes(f.Data), false, s.obs); err != nil {
			return fmt.Errorf("failed to remove html/%s of artifact %q: %w", f.Path, name, err)
		}
		for d := path.Dir(f.Path); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	list := make([]string, 0, len(dirs))
	for d := range dirs {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return strings.Count(list[i], "/") > strings.Count(list[j], "/") })
	for _, d := range list {
		filestore.RemoveEmptyDir(s.root, rel(name, HTMLDir+"/"+d))
	}
	filestore.RemoveEmptyDir(s.root, rel(name, HTMLDir))
	return nil
}

// restorePageFiles undoes files this Put wrote: a file that did not exist is
// removed, one that did gets its old bytes back, each only if it is still the
// bytes this Put wrote. Errors are ignored: the original failure is the one
// worth reporting.
func (s *FileStore) restorePageFiles(name string, wrote, was []PageFile) {
	old := map[string][]byte{}
	for _, f := range was {
		old[f.Path] = f.Data
	}
	for _, f := range wrote {
		h := filestore.HashBytes(f.Data)
		if prev, ok := old[f.Path]; ok {
			_, _ = filestore.WriteAtomic(s.root, filestore.Write{Rel: pageFileRel(name, f.Path), Data: prev, Expect: h}, s.obs)
			continue
		}
		_ = filestore.Remove(s.root, pageFileRel(name, f.Path), h, false, s.obs)
	}
}

// readPageFile reads one page file without following a link: the path is
// checked with lstat, opened, and the open file must be the very file lstat
// saw (a link swapped in between is a different file and is refused).
func readPageFile(root, relPath string, listed fs.FileInfo, limit int64) ([]byte, time.Time, error) {
	abs, err := filestore.Resolve(root, relPath)
	if err != nil {
		return nil, time.Time{}, errors.New("is not inside the workspace")
	}
	before, err := os.Lstat(abs)
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(before, listed) {
		return nil, time.Time{}, errors.New("is not a regular file (or changed while it was read)")
	}
	f, err := os.Open(abs) //nolint:gosec // the path was resolved inside the workspace and is checked against lstat below
	if err != nil {
		return nil, time.Time{}, errors.New("cannot be opened")
	}
	defer f.Close() //nolint:errcheck
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, time.Time{}, errors.New("is not a regular file (or changed while it was read)")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, time.Time{}, errors.New("cannot be read")
	}
	if int64(len(data)) > limit {
		return nil, time.Time{}, fmt.Errorf("is over the %d byte limit", limit)
	}
	return data, opened.ModTime(), nil
}
