package notebooks

import "context"

// sqliteStore is the legacy container's store: it wraps the package-level
// _vibe_notebooks functions unchanged. It has no compare-and-write (the
// container holds an exclusive lock, so there is no second writer) and
// ignores SaveOptions.
type sqliteStore struct{ db DB }

// NewSQLiteStore returns the Store over a container's _vibe_notebooks table.
func NewSQLiteStore(db DB) Store { return &sqliteStore{db: db} }

func (s *sqliteStore) List(context.Context) ([]Summary, error) { return List(s.db) }

func (s *sqliteStore) Get(_ context.Context, id string) (Notebook, []string, error) {
	return Get(s.db, id)
}

func (s *sqliteStore) Save(_ context.Context, raw any, _ ...SaveOption) (Notebook, []string, error) {
	return Save(s.db, raw)
}

func (s *sqliteStore) Create(_ context.Context, title string) (Notebook, error) {
	return Create(s.db, title)
}

func (s *sqliteStore) Insert(_ context.Context, nb Notebook) (Notebook, []string, error) {
	return Insert(s.db, nb)
}

func (s *sqliteStore) Rename(_ context.Context, id, title string) error {
	return Rename(s.db, id, title)
}

func (s *sqliteStore) Delete(_ context.Context, id string) error { return Delete(s.db, id) }
