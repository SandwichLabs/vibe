package skills

import (
	"context"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Skill sources.
const (
	SourceBuiltin = "built-in"
	SourceUser    = "user"
	SourceImport  = "import"
)

// Stored is one user or import skill as a Store holds it. Built-ins are
// embedded, never stored. Validation (name rule, size caps, shadowing a
// built-in) belongs to the caller (skills.SaveUserSkillAt); a Store only
// refuses what it cannot represent safely.
type Stored struct {
	Name         string
	Description  string
	Instructions string
	TableName    string // when set, the skill loads only if that table has rows
	Source       string // SourceUser | SourceImport
	// Front is the file's frontmatter as last read, one entry per line; Put
	// rewrites it with the known keys updated and every other line (keys such
	// as allowed-tools or license, comments, their order) kept as it was.
	// Callers copy it through unchanged. Empty for a new skill.
	Front []string
	// Error is set by the file store for a skill folder that could not be read
	// (no valid frontmatter, name mismatch, too large): List still returns it,
	// with the folder name as Name, so it can be found and fixed. Consumers
	// that load skills for the agent skip such entries.
	Error     string
	CreatedAt string
	UpdatedAt string
	// Rev is the file store's revision as loaded (content hash); Put refuses a
	// stale one with ErrConflict. Empty for the legacy container store.
	Rev string
}

// Store is where a workspace keeps its skills. The file store keeps each as
// skills/<name>/SKILL.md (the same frontmatter the built-ins use, so Claude
// Code can read them); the SQLite store wraps the legacy _vibe_skills table.
type Store interface {
	// List returns every stored skill ordered by name; never nil-erroring on
	// an empty or missing store.
	List(ctx context.Context) ([]Stored, error)
	// Get returns one skill; a missing name satisfies errors.Is(err, ErrNotFound).
	Get(ctx context.Context, name string) (*Stored, error)
	// Put creates or replaces a skill. With a file store a stale write (s.Rev,
	// WithRev, or the revision last seen for the name no longer matches the
	// file) returns ErrConflict and changes nothing; writing blind over an
	// existing file this store never read is also a conflict, so callers do a
	// Get first (or pass Overwrite).
	Put(ctx context.Context, s *Stored, opts ...PutOption) error
	// Delete removes a skill; a missing name is a no-op.
	Delete(ctx context.Context, name string) error
}

// ErrConflict means the skill changed on disk since it was loaded.
var ErrConflict = filestore.ErrConflict

// ErrNotFound means there is no such skill.
var ErrNotFound = filestore.ErrNotFound

// PutOption tunes one Put.
type PutOption func(*putOpts)

type putOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
}

// Overwrite skips the conflict check.
func Overwrite() PutOption { return func(o *putOpts) { o.overwrite = true } }

// WithRev sets the revision the caller loaded, overriding s.Rev.
func WithRev(rev string) PutOption {
	return func(o *putOpts) { o.rev, o.hasRev = rev, true }
}

func applyPutOpts(opts []PutOption) putOpts {
	var o putOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}
