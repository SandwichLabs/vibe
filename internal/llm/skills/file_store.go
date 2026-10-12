package skills

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Dir is the workspace directory skills live in; File the document name.
const (
	Dir  = "skills"
	File = "SKILL.md"
)

// FileStore keeps skills as skills/<name>/SKILL.md under a workspace root.
//
// # File format
//
// The same frontmatter the built-ins use, so Claude Code and any
// agentskills.io-style tool can read the file:
//
//	---
//	name: revenue-definitions
//	description: One line the system prompt advertises
//	table_name: orders        (only when the skill is table-gated)
//	source: import            (only when not "user")
//	---
//
//	# Markdown instructions ...
//
// The directory name is the skill name and must equal the frontmatter name; a
// mismatch is skipped (logged by the caller's List consumer as a missing
// skill) rather than guessed at. Timestamps are not stored; UpdatedAt is the
// file's mtime. Sibling files in the skill directory are left alone.
//
// Conflict handling and identity follow notebooks.FileStore.
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

// WithLogger sets where the store reports files it normalises (default slog.Default()).
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

func rel(name string) string { return Dir + "/" + name + "/" + File }

// knownKeys are the frontmatter keys the store owns, in the order a new skill
// is written.
var knownKeys = []string{"name", "description", "table_name", "source"}

func frontKey(line string) string {
	k, _, ok := strings.Cut(line, ":")
	if !ok || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	return strings.TrimSpace(k)
}

// Encode renders a stored skill as SKILL.md bytes. When st.Front is set (the
// skill was read from a file) every line it holds that is not one of the
// store's own keys is kept verbatim and in place, so a skill authored with
// Claude Code round-trips without losing allowed-tools, license, comments or
// their order. Only blank lines around the body are trimmed.
func Encode(st Stored) ([]byte, error) {
	for field, v := range map[string]string{"name": st.Name, "description": st.Description, "table_name": st.TableName, "source": st.Source} {
		if strings.ContainsAny(v, "\n\r") {
			return nil, fmt.Errorf("skill %s must be a single line", field)
		}
	}
	if strings.TrimSpace(st.Description) == "" {
		return nil, fmt.Errorf("skill description is required")
	}
	values := map[string]string{
		"name": st.Name, "description": strings.TrimSpace(st.Description), "table_name": strings.TrimSpace(st.TableName),
	}
	if st.Source != "" && st.Source != SourceUser {
		values["source"] = st.Source
	}
	emitted := map[string]bool{}
	var lines []string
	for _, raw := range st.Front {
		k := frontKey(raw)
		isKnown := false
		for _, kk := range knownKeys {
			isKnown = isKnown || kk == k
		}
		switch {
		case !isKnown:
			lines = append(lines, raw)
		case values[k] != "" && !emitted[k]:
			lines = append(lines, k+": "+values[k])
			emitted[k] = true
		}
	}
	for _, k := range knownKeys {
		if values[k] != "" && !emitted[k] {
			lines = append(lines, k+": "+values[k])
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(TrimBlankLines(strings.ReplaceAll(st.Instructions, "\r\n", "\n")))
	b.WriteString("\n")
	return []byte(b.String()), nil
}

// Decode parses SKILL.md bytes for the skill in directory name.
func Decode(name string, data []byte, logger *slog.Logger) (Stored, error) {
	doc, ok := ParseDocument(string(data))
	if !ok {
		return Stored{}, fmt.Errorf("skill %q has no valid frontmatter (name and description are required)", name)
	}
	if doc.Meta["name"] != name {
		return Stored{}, fmt.Errorf("skill directory %q does not match its frontmatter name %q", name, doc.Meta["name"])
	}
	// A file may only claim to be a user or an import skill. "built-in" is
	// reserved for what is embedded in the binary: a repository's skill that
	// says otherwise would borrow the built-in badge (no edit, no delete) and
	// the agent's trust in it.
	src := doc.Meta["source"]
	switch src {
	case SourceUser, SourceImport:
	case "":
		src = SourceUser
	default:
		if logger != nil {
			logger.Warn("skill file claims an unknown source; treating it as a user skill", "skill", name, "source", src)
		}
		src = SourceUser
	}
	return Stored{
		Name: name, Description: doc.Meta["description"], Instructions: doc.RawBody,
		TableName: doc.Meta["table_name"], Source: src, Front: doc.Front,
	}, nil
}

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

func (s *FileStore) load(name string) (Stored, error) {
	data, st, err := filestore.Read(s.root, rel(name))
	if err != nil {
		return Stored{}, err
	}
	sk, err := Decode(name, data, s.logger)
	if err != nil {
		return Stored{}, err
	}
	ts := st.ModTime.UTC().Format(time.RFC3339)
	sk.CreatedAt, sk.UpdatedAt, sk.Rev = ts, ts, st.Rev()
	return sk, nil
}

// List implements Store. A skill directory that cannot be read as a skill is
// returned with Error set (and is also named in ListWithSkipped's map).
func (s *FileStore) List(ctx context.Context) ([]Stored, error) {
	out, _, err := s.ListWithSkipped(ctx)
	return out, err
}

// ListWithSkipped is List plus the directory names that were skipped and why.
func (s *FileStore) ListWithSkipped(ctx context.Context) ([]Stored, map[string]string, error) {
	dirs, err := filestore.ListSubdirs(s.root, Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list skills: %w", err)
	}
	var out []Stored
	skipped := map[string]string{}
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		sk, err := s.load(d)
		if err != nil {
			skipped[d] = err.Error()
			out = append(out, Stored{Name: d, Source: SourceUser, Error: err.Error()})
			continue
		}
		s.remember(d, sk.Rev)
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, skipped, nil
}

// Get implements Store.
func (s *FileStore) Get(_ context.Context, name string) (*Stored, error) {
	if !NamePattern.MatchString(name) {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	sk, err := s.load(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get skill %q: %w", name, err)
	}
	s.remember(name, sk.Rev)
	return &sk, nil
}

// Put implements Store.
func (s *FileStore) Put(_ context.Context, st *Stored, opts ...PutOption) error {
	o := applyPutOpts(opts)
	if !NamePattern.MatchString(st.Name) {
		return fmt.Errorf("invalid skill name %q: must match %s", st.Name, NamePattern.String())
	}
	if st.Source == "" {
		st.Source = SourceUser
	}
	expect := st.Rev
	if o.hasRev {
		expect = o.rev
	}
	if expect == "" && !o.overwrite {
		expect = s.lastSeen(st.Name)
	}
	if st.Front == nil {
		// A caller that never held the file (the agent) still must not drop
		// the frontmatter keys it does not know about: carry them over from
		// what is on disk now. If that differs from what the caller saw, the
		// compare-and-write below refuses the save anyway.
		if cur, err := s.load(st.Name); err == nil {
			st.Front = cur.Front
		}
	}
	data, err := Encode(*st)
	if err != nil {
		return err
	}
	stamp, err := filestore.WriteAtomic(s.root, filestore.Write{Rel: rel(st.Name), Data: data, Expect: expect, Overwrite: o.overwrite}, s.obs)
	if err != nil {
		return fmt.Errorf("failed to save skill %q: %w", st.Name, err)
	}
	ts := stamp.ModTime.UTC().Format(time.RFC3339)
	if st.CreatedAt == "" {
		st.CreatedAt = ts
	}
	st.UpdatedAt, st.Rev = ts, stamp.Rev()
	s.remember(st.Name, st.Rev)
	return nil
}

// Delete implements Store: it removes SKILL.md, and the directory when that
// leaves it empty (other files a user keeps beside it are never touched).
func (s *FileStore) Delete(_ context.Context, name string) error {
	if !NamePattern.MatchString(name) {
		return nil
	}
	if err := filestore.Remove(s.root, rel(name), s.lastSeen(name), false, s.obs); err != nil {
		return fmt.Errorf("failed to delete skill %q: %w", name, err)
	}
	filestore.RemoveEmptyDir(s.root, Dir+"/"+name)
	s.mu.Lock()
	delete(s.seen, name)
	s.mu.Unlock()
	return nil
}
