package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SQLiteDB is the *sql.DB surface the legacy store needs.
type SQLiteDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type sqliteStore struct{ db SQLiteDB }

// NewSQLiteStore returns the Store over a container's _vibe_skills table. A
// nil db is an empty store (some callers have no container).
func NewSQLiteStore(db SQLiteDB) Store { return &sqliteStore{db: db} }

func (s *sqliteStore) List(ctx context.Context) ([]Stored, error) {
	if s.isNil() {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, description, instructions, table_name, source, created_at, updated_at FROM _vibe_skills ORDER BY name`)
	if err != nil {
		// A pre-#62 file that has not been migrated has no table: that is an
		// empty list, not a failure of the whole skills listing.
		return nil, nil //nolint:nilerr
	}
	defer rows.Close() //nolint:errcheck
	var out []Stored
	for rows.Next() {
		var e Stored
		if err := rows.Scan(&e.Name, &e.Description, &e.Instructions, &e.TableName, &e.Source, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan skill row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) isNil() bool { return s == nil || s.db == nil }

func (s *sqliteStore) Get(ctx context.Context, name string) (*Stored, error) {
	if s.isNil() {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	var e Stored
	err := s.db.QueryRowContext(ctx,
		`SELECT name, description, instructions, table_name, source, created_at, updated_at FROM _vibe_skills WHERE name = ?`, name).
		Scan(&e.Name, &e.Description, &e.Instructions, &e.TableName, &e.Source, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get skill %q: %w", name, err)
	}
	return &e, nil
}

func (s *sqliteStore) Put(ctx context.Context, st *Stored, _ ...PutOption) error {
	if s.isNil() {
		return fmt.Errorf("no open container")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	src := st.Source
	if src == "" {
		src = SourceUser
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO _vibe_skills (name, description, instructions, table_name, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   description = excluded.description,
		   instructions = excluded.instructions,
		   table_name = excluded.table_name,
		   source = excluded.source,
		   updated_at = excluded.updated_at`,
		st.Name, st.Description, st.Instructions, st.TableName, src, now, now)
	if err != nil {
		return fmt.Errorf("failed to save skill: %w", err)
	}
	st.Source, st.UpdatedAt = src, now
	return nil
}

func (s *sqliteStore) Delete(ctx context.Context, name string) error {
	if s.isNil() {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM _vibe_skills WHERE name = ?`, name); err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}
	return nil
}
