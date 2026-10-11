package notebooks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// FileStore keeps notebooks as notebooks/<id>.vnb under a workspace root.
//
// # Identity
//
// A notebook's id is its file stem ("weekly-revenue" for
// notebooks/weekly-revenue.vnb). The file carries no id of its own, so a
// rename in an editor or `git mv` is simply a new id, and nothing inside a
// file can disagree with where it lives. Create derives the stem from the
// title and picks the first free "-2", "-3", ... when it is taken. Rename
// changes the title only and keeps the file name, so ids stay stable for
// links, the agent's active-notebook tracking and the cell-level
// `notebook_id` the agent passes around.
//
// # Conflicts
//
// Get returns the revision it read (Notebook.Rev, the content hash) and
// remembers it per id. Save compares the file on disk against, in order: the
// IfRev option, the notebook's own Rev, the revision this store last saw for
// the id. A mismatch returns ErrConflict and writes nothing. A save with none
// of those against a file that already exists is also a conflict, so
// two writers can never silently clobber each other.
//
// The in-process "last seen" fallback (refreshed by Get, List and every write)
// exists only for internal callers that read and write in one breath (the
// agent's add_query_cell). Anything that holds a copy across user time, Studio
// above all, must send the Rev it loaded, so an agent write in the same
// process still conflicts with a stale editor.
type FileStore struct {
	root string
	obs  filestore.Observer

	mu   sync.Mutex
	seen map[string]string // id -> last revision seen or written
}

// FileStoreOption configures NewFileStore.
type FileStoreOption func(*FileStore)

// WithObserver reports this store's own writes and deletes (absolute path and
// content hash) so a file watcher can ignore them.
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

func rel(id string) string { return Dir + "/" + id + Extension }

func (s *FileStore) remember(id, rev string) {
	s.mu.Lock()
	s.seen[id] = rev
	s.mu.Unlock()
}

func (s *FileStore) lastSeen(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[id]
}

// List implements Store.
func (s *FileStore) List(ctx context.Context) ([]Summary, error) {
	names, err := filestore.ListDir(s.root, Dir, Extension)
	if err != nil {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	out := make([]Summary, 0, len(names))
	for _, n := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(n, Extension)
		sum := Summary{ID: id, Title: id}
		if !filestore.ValidID(id) {
			sum.Error = "the file name is not a valid notebook id: use lowercase letters, digits, - and _"
			out = append(out, sum)
			continue
		}
		data, st, err := filestore.Read(s.root, rel(id))
		if err != nil {
			if errors.Is(err, filestore.ErrNotFound) {
				continue // vanished between ReadDir and Read
			}
			sum.Error = err.Error()
			out = append(out, sum)
			continue
		}
		s.remember(id, st.Rev())
		sum.UpdatedAt = st.ModTime.UTC().Format(time.RFC3339)
		if nb, _, cerr := Coerce(data); cerr != nil {
			sum.Error = cerr.Error()
		} else {
			sum.Title, sum.CellCount = nb.Title, len(nb.Cells)
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// Get implements Store.
func (s *FileStore) Get(_ context.Context, id string) (Notebook, []string, error) {
	if !filestore.ValidID(id) {
		return Notebook{}, nil, fmt.Errorf("failed to get notebook %q: invalid id", id)
	}
	data, st, err := filestore.Read(s.root, rel(id))
	if err != nil {
		return Notebook{}, nil, fmt.Errorf("failed to get notebook %q: %w", id, err)
	}
	nb, warnings, err := Coerce(data)
	if err != nil {
		return Notebook{}, warnings, fmt.Errorf("notebook %q is not valid: %w", id, err)
	}
	nb.ID = id
	nb.UpdatedAt = st.ModTime.UTC().Format(time.RFC3339)
	nb.Rev = st.Rev()
	s.remember(id, nb.Rev)
	return nb, warnings, nil
}

var autoID = regexp.MustCompile(`^nb_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// freeID picks an unused stem for a notebook that has no real id yet.
func (s *FileStore) freeID(title string) string {
	return filestore.UniqueSlug(filestore.Slug(title), func(id string) bool {
		_, err := filestore.StatStamp(s.root, rel(id))
		return err == nil
	})
}

// Save implements Store.
func (s *FileStore) Save(_ context.Context, raw any, opts ...SaveOption) (Notebook, []string, error) {
	o := applySaveOpts(opts)
	nb, warnings, err := Coerce(raw)
	if err != nil {
		return Notebook{}, warnings, err
	}
	expect := nb.Rev
	if o.hasRev {
		expect = o.rev
	}
	id := nb.ID
	if autoID.MatchString(id) && !o.keepID {
		// A brand-new notebook (Empty, a template): name the file after the
		// title instead of an opaque uuid.
		id = s.freeID(nb.Title)
		expect = ""
	}
	if !filestore.ValidID(id) {
		return Notebook{}, warnings, fmt.Errorf("failed to save notebook: invalid id %q", id)
	}
	if expect == "" && !o.overwrite {
		expect = s.lastSeen(id)
	}
	return s.write(id, nb, expect, o.overwrite, warnings)
}

func (s *FileStore) write(id string, nb Notebook, expect string, overwrite bool, warnings []string) (Notebook, []string, error) {
	data, err := EncodeFile(nb)
	if err != nil {
		return Notebook{}, warnings, err
	}
	st, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(id), Data: data, Expect: expect, Overwrite: overwrite}, s.obs)
	if err != nil {
		return Notebook{}, warnings, fmt.Errorf("failed to save notebook %q: %w", id, err)
	}
	nb.ID = id
	nb.UpdatedAt = st.ModTime.UTC().Format(time.RFC3339)
	nb.Rev = st.Rev()
	nb.SchemaVersion = SchemaVersion
	s.remember(id, nb.Rev)
	return nb, warnings, nil
}

// Create implements Store.
func (s *FileStore) Create(ctx context.Context, title string) (Notebook, error) {
	nb := Empty(title)
	saved, _, err := s.Save(ctx, notebookToRaw(nb))
	return saved, err
}

// Insert implements Store.
func (s *FileStore) Insert(ctx context.Context, nb Notebook) (Notebook, []string, error) {
	nb.Rev = ""
	return s.Save(ctx, notebookToRaw(nb))
}

// Rename implements Store.
func (s *FileStore) Rename(_ context.Context, id, title string) error {
	if title == "" {
		title = "Untitled notebook"
	}
	data, st, err := filestore.Read(s.root, rel(id))
	if err != nil {
		return fmt.Errorf("failed to rename notebook %q: %w", id, err)
	}
	nb, _, err := Coerce(data)
	if err != nil {
		return fmt.Errorf("notebook %q is not valid: %w", id, err)
	}
	nb.Title, _ = clampRunes(title, maxTitleRunes)
	if _, _, err := s.write(id, nb, st.Rev(), false, nil); err != nil {
		return err
	}
	return nil
}

// Delete implements Store.
func (s *FileStore) Delete(_ context.Context, id string) error {
	if !filestore.ValidID(id) {
		return fmt.Errorf("failed to delete notebook %q: invalid id", id)
	}
	if err := filestore.Remove(s.root, rel(id), s.lastSeen(id), false, s.obs); err != nil {
		return fmt.Errorf("failed to delete notebook %q: %w", id, err)
	}
	s.mu.Lock()
	delete(s.seen, id)
	s.mu.Unlock()
	return nil
}
