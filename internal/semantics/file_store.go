package semantics

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// FileStore keeps definitions under a workspace root:
//
//	semantics/entities/<name>.yaml
//	semantics/metrics/<name>.yaml
//	semantics/joins/<name>.yaml
//	semantics/glossary.md
//
// The file name is the identity and equals the document's name field (a
// mismatch lists with Error). Writes are filestore compare-and-write; nothing
// is cached, so every read sees the files as they are. Like the other stores
// it creates folders on first write; an empty or missing semantics/ is simply
// no definitions.
type FileStore struct {
	*core
}

// FileStoreOption configures NewFileStore.
type FileStoreOption func(*fileBackend, *core)

// WithObserver reports this store's own writes so a watcher can ignore them.
func WithObserver(o filestore.Observer) FileStoreOption {
	return func(b *fileBackend, _ *core) { b.obs = o }
}

// WithLogger sets where the store reports files it lists with an error
// (default slog.Default()).
func WithLogger(l *slog.Logger) FileStoreOption {
	return func(_ *fileBackend, c *core) {
		if l != nil {
			c.logger = l
		}
	}
}

// NewFileStore returns the file-backed Store rooted at a workspace folder.
func NewFileStore(root string, opts ...FileStoreOption) *FileStore {
	b := &fileBackend{root: root}
	c := &core{b: b, logger: slog.Default()}
	for _, o := range opts {
		o(b, c)
	}
	return &FileStore{core: c}
}

var _ Store = (*FileStore)(nil)

type fileBackend struct {
	root string
	obs  filestore.Observer
}

func (f *fileBackend) read(rel string) ([]byte, string, error) {
	data, st, err := filestore.Read(f.root, rel)
	return data, st.Rev(), err
}

func (f *fileBackend) write(rel string, data []byte, expect string, overwrite bool) (string, error) {
	st, err := filestore.WriteAtomic(f.root, filestore.Write{Rel: rel, Data: data, Expect: expect, Overwrite: overwrite}, f.obs)
	return st.Rev(), err
}

func (f *fileBackend) remove(rel, expect string, overwrite bool) error {
	return filestore.Remove(f.root, rel, expect, overwrite, f.obs)
}

func (f *fileBackend) names(dir string) ([]fileName, error) {
	abs, err := filestore.Resolve(f.root, Dir+"/"+dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list %s/%s: %w", Dir, dir, err)
	}
	var out []fileName
	for _, e := range entries {
		if filestore.IsTemp(e.Name()) || !strings.HasSuffix(e.Name(), Extension) {
			continue
		}
		switch {
		case e.Type().IsRegular():
			out = append(out, fileName{name: e.Name()})
		case e.Type()&fs.ModeSymlink != 0:
			out = append(out, fileName{name: e.Name(), link: true})
		}
	}
	return out, nil
}
