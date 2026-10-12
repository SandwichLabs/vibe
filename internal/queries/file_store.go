package queries

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/connname"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/jsonenc"
)

// Extension is the query file extension; Dir the workspace directory.
const (
	Extension = ".sql"
	Dir       = "queries"
	// HeaderPrefix starts the first line of a query file.
	HeaderPrefix = "-- vibe:"
)

// FileStore keeps queries as queries/<id>.sql under a workspace root.
//
// # File format
//
//	-- vibe: {"title":"Top customers","description":"...","connection":"prod_pg","parameters":[...]}
//	SELECT ...
//
// The first line is a SQL comment followed by one line of JSON (so any SQL
// tool still runs the file, and the header stays one diff line). Keys:
// title, description, connection, parameters; unknown keys are ignored and
// dropped on the next save. A file with no header is a valid query titled by
// its file stem. The body is the SQL as written, ending in one newline.
// Timestamps are not stored: UpdatedAt is the file's mtime.
//
// # Identity and conflicts
//
// Same rules as notebooks.FileStore: the id is the file stem (Create takes it
// from the title, "-2", "-3" when taken; Update never renames the file), and
// writes compare the file's content hash against the revision the caller
// loaded. The in-process "last seen" revision (refreshed by Get, List and every
// write) is a fallback for internal callers only; a caller that holds a query
// across user time, Studio above all, sends the Rev it loaded.
//
// A file whose header cannot be parsed lists and loads with Error set (the
// whole file as SQL, so it can still be read and run) and refuses saves: the
// store never rewrites a file it could not fully read, so parameters it failed
// to parse are never silently dropped or altered.
type FileStore struct {
	root   string
	obs    filestore.Observer
	logger *slog.Logger

	mu   sync.Mutex
	seen map[string]string
}

// FileStoreOption configures NewFileStore.
type FileStoreOption func(*FileStore)

// WithObserver reports this store's own writes so a watcher can ignore them.
func WithObserver(o filestore.Observer) FileStoreOption {
	return func(s *FileStore) { s.obs = o }
}

// WithLogger sets where the store reports files it skips (default slog.Default()).
func WithLogger(l *slog.Logger) FileStoreOption {
	return func(s *FileStore) {
		if l != nil {
			s.logger = l
		}
	}
}

// NewFileStore returns the file-backed Store rooted at a workspace folder.
func NewFileStore(root string, opts ...FileStoreOption) *FileStore {
	s := &FileStore{root: root, seen: map[string]string{}, logger: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

var _ Store = (*FileStore)(nil)

type header struct {
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Connection  string  `json:"connection,omitempty"`
	Parameters  []Param `json:"parameters,omitempty"`
}

// Encode renders a query as file bytes (see FileStore).
func Encode(q Query) ([]byte, error) {
	h := header{Title: q.Title, Description: q.Description, Connection: q.Connection, Parameters: q.Parameters}
	if len(h.Parameters) == 0 {
		h.Parameters = nil
	}
	hb, err := jsonenc.Encode(h, "")
	if err != nil {
		return nil, fmt.Errorf("failed to encode query header: %w", err)
	}
	body := strings.TrimRight(strings.ReplaceAll(q.SQL, "\r\n", "\n"), "\n")
	var out bytes.Buffer
	out.WriteString(HeaderPrefix + " ")
	out.Write(bytes.TrimRight(hb, "\n"))
	out.WriteString("\n")
	out.WriteString(body)
	out.WriteString("\n")
	return out.Bytes(), nil
}

// Decode parses file bytes. id is the file stem (the title of a header-less
// file).
func Decode(id string, data []byte) (Query, error) {
	// A BOM (Windows editors) and blank or indented lines before the header are
	// not part of the query.
	text := strings.TrimLeft(strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\uFEFF"), " \t\n")
	q := Query{ID: id, Title: id, Engine: "duckdb", Parameters: []Param{}}
	first, rest, _ := strings.Cut(text, "\n")
	if strings.HasPrefix(first, HeaderPrefix) {
		var h header
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(first, HeaderPrefix))), &h); err != nil {
			// Hand back what is there: the whole file as SQL, with Error set, so
			// the query can still be read and run. Saves are refused (see
			// Update) because rewriting would drop or alter the header the
			// store could not read.
			q.SQL = strings.TrimRight(text, "\n")
			q.Error = fmt.Sprintf("the -- vibe: header could not be read: %v", err)
			return q, nil
		}
		if strings.TrimSpace(h.Title) != "" {
			q.Title = h.Title
		}
		q.Description, q.Connection = h.Description, h.Connection
		if h.Parameters != nil {
			q.Parameters = h.Parameters
		}
		text = rest
	}
	q.SQL = strings.TrimRight(strings.TrimLeft(text, "\n"), "\n")
	return q, nil
}

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

func (s *FileStore) load(id string) (Query, filestore.Stamp, error) {
	data, st, err := filestore.Read(s.root, rel(id))
	if err != nil {
		return Query{}, st, err
	}
	q, err := Decode(id, data)
	if err != nil {
		return Query{}, st, err
	}
	ts := st.ModTime.UTC().Format(time.RFC3339)
	q.CreatedAt, q.UpdatedAt, q.Rev = ts, ts, st.Rev()
	return q, st, nil
}

// List implements Store.
func (s *FileStore) List(ctx context.Context) ([]Query, error) {
	names, err := filestore.ListDir(s.root, Dir, Extension)
	if err != nil {
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}
	out := make([]Query, 0, len(names))
	for _, n := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(n, Extension)
		if !filestore.ValidID(id) {
			out = append(out, Query{ID: id, Title: id, Engine: "duckdb", Parameters: []Param{},
				Error: "the file name is not a valid query id: use lowercase letters, digits, - and _"})
			continue
		}
		q, _, err := s.load(id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // vanished between ReadDir and Read
			}
			s.logger.Warn("unreadable query file", "id", id, "error", err)
			out = append(out, Query{ID: id, Title: id, Engine: "duckdb", Parameters: []Param{}, Error: err.Error()})
			continue
		}
		s.remember(id, q.Rev)
		out = append(out, q)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Get implements Store.
func (s *FileStore) Get(_ context.Context, id string) (*Query, error) {
	if !filestore.ValidID(id) {
		return nil, fmt.Errorf("failed to get query %q: invalid id", id)
	}
	q, _, err := s.load(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get query %q: %w", id, err)
	}
	s.remember(id, q.Rev)
	return &q, nil
}

var uuidLike = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validate(q *Query) error {
	if strings.TrimSpace(q.Title) == "" {
		return fmt.Errorf("query title is required")
	}
	if q.Connection != "" {
		if err := connname.Validate(q.Connection); err != nil {
			return err
		}
	}
	return nil
}

// Create implements Store.
func (s *FileStore) Create(_ context.Context, q *Query) error {
	if err := validate(q); err != nil {
		return err
	}
	id := q.ID
	if id == "" || uuidLike.MatchString(id) {
		id = filestore.UniqueSlug(filestore.Slug(q.Title), func(c string) bool {
			_, err := filestore.StatStamp(s.root, rel(c))
			return err == nil
		})
	}
	if !filestore.ValidID(id) {
		return fmt.Errorf("failed to create query: invalid id %q", id)
	}
	data, err := Encode(*q)
	if err != nil {
		return err
	}
	// Expect "" = the file must not exist: a Create never clobbers.
	st, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(id), Data: data}, s.obs)
	if err != nil {
		return fmt.Errorf("failed to create query %q: %w", id, err)
	}
	q.ID, q.Engine = id, "duckdb"
	ts := st.ModTime.UTC().Format(time.RFC3339)
	q.CreatedAt, q.UpdatedAt, q.Rev = ts, ts, st.Rev()
	s.remember(id, q.Rev)
	return nil
}

// Update implements Store.
func (s *FileStore) Update(_ context.Context, q *Query, opts ...WriteOption) error {
	o := applyWriteOpts(opts)
	if !filestore.ValidID(q.ID) {
		return fmt.Errorf("failed to update query: invalid id %q", q.ID)
	}
	if err := validate(q); err != nil {
		return err
	}
	cur, curStamp, err := s.load(q.ID)
	if err != nil {
		if errorsIsNotFound(err) {
			return &notFoundError{id: q.ID}
		}
		return fmt.Errorf("failed to update query %q: %w", q.ID, err)
	}
	if cur.Error != "" {
		return fmt.Errorf("failed to update query %q: not saved, because that would replace a header that could not be read (%s); fix the file first", q.ID, cur.Error)
	}
	expect := q.Rev
	if o.hasRev {
		expect = o.rev
	}
	if expect == "" {
		expect = s.lastSeen(q.ID)
	}
	if expect == "" && !o.overwrite {
		// Never loaded by this store and no revision given: the caller is
		// writing blind over a file that exists.
		return fmt.Errorf("failed to update query %q: %w", q.ID,
			&filestore.ConflictError{Path: rel(q.ID), Existing: true, Got: curStamp.Rev()})
	}
	data, err := Encode(*q)
	if err != nil {
		return err
	}
	st, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(q.ID), Data: data, Expect: expect, Overwrite: o.overwrite}, s.obs)
	if err != nil {
		return fmt.Errorf("failed to update query %q: %w", q.ID, err)
	}
	ts := st.ModTime.UTC().Format(time.RFC3339)
	q.Engine, q.UpdatedAt, q.Rev = "duckdb", ts, st.Rev()
	if q.CreatedAt == "" {
		q.CreatedAt = ts
	}
	s.remember(q.ID, q.Rev)
	return nil
}

// Delete implements Store.
func (s *FileStore) Delete(_ context.Context, id string) error {
	if !filestore.ValidID(id) {
		return fmt.Errorf("failed to delete query %q: invalid id", id)
	}
	if _, err := filestore.StatStamp(s.root, rel(id)); err != nil {
		if errorsIsNotFound(err) {
			return &notFoundError{id: id}
		}
		return fmt.Errorf("failed to delete query %q: %w", id, err)
	}
	if err := filestore.Remove(s.root, rel(id), s.lastSeen(id), false, s.obs); err != nil {
		return fmt.Errorf("failed to delete query %q: %w", id, err)
	}
	s.mu.Lock()
	delete(s.seen, id)
	s.mu.Unlock()
	return nil
}
