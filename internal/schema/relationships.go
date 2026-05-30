package schema

// Relationship definitions (plan §2.1/§4.5). Relationships are stored in the
// SQLCipher container's _vibe_relationships table and are the metadata source
// of truth that the relationship diagram and FK-emit logic read from. The
// actual DuckDB constraint is applied separately (P2.3).

import (
	"database/sql"
	"fmt"
	"time"
)

// Relationship is a stored foreign-key relationship definition:
// child.<ChildColumn> references parent.<ParentColumn>.
type Relationship struct {
	ID               string `json:"id"`
	ParentTable      string `json:"parent_table"`
	ParentColumn     string `json:"parent_column"`
	ChildTable       string `json:"child_table"`
	ChildColumn      string `json:"child_column"`
	OnDelete         string `json:"on_delete"` // RESTRICT | CASCADE | SET NULL | NO ACTION
	OnUpdate         string `json:"on_update"`
	EnforceIntegrity bool   `json:"enforce_integrity"`
	CreatedAt        string `json:"created_at"`
}

// RelationshipStore provides read/write access to relationship definitions
// stored in the SQLCipher container's _vibe_relationships table.
type RelationshipStore struct {
	db *sql.DB
}

// NewRelationshipStore constructs a relationship store over the given SQLCipher
// container DB.
func NewRelationshipStore(db *sql.DB) *RelationshipStore {
	return &RelationshipStore{db: db}
}

// relationshipColumns is the canonical SELECT column list, in scan order.
const relationshipColumns = `id, parent_table, parent_column, child_table,
	child_column, on_delete, on_update, enforce_integrity, created_at`

// scanRelationship scans a single row into a Relationship.
func scanRelationship(rows *sql.Rows) (Relationship, error) {
	var rel Relationship
	var enforce int
	if err := rows.Scan(
		&rel.ID, &rel.ParentTable, &rel.ParentColumn, &rel.ChildTable,
		&rel.ChildColumn, &rel.OnDelete, &rel.OnUpdate, &enforce, &rel.CreatedAt,
	); err != nil {
		return Relationship{}, fmt.Errorf("failed to scan relationship: %w", err)
	}
	rel.EnforceIntegrity = enforce != 0
	return rel, nil
}

// Save upserts a relationship definition keyed by ID. created_at is preserved
// on update and populated on first insert.
func (s *RelationshipStore) Save(rel Relationship) error {
	enforce := 0
	if rel.EnforceIntegrity {
		enforce = 1
	}
	if rel.CreatedAt == "" {
		rel.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := s.db.Exec(
		`INSERT INTO _vibe_relationships (
			id, parent_table, parent_column, child_table, child_column,
			on_delete, on_update, enforce_integrity, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			parent_table      = excluded.parent_table,
			parent_column     = excluded.parent_column,
			child_table       = excluded.child_table,
			child_column      = excluded.child_column,
			on_delete         = excluded.on_delete,
			on_update         = excluded.on_update,
			enforce_integrity = excluded.enforce_integrity;`,
		rel.ID, rel.ParentTable, rel.ParentColumn, rel.ChildTable, rel.ChildColumn,
		rel.OnDelete, rel.OnUpdate, enforce, rel.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert relationship %q: %w", rel.ID, err)
	}
	return nil
}

// Get reads a stored relationship definition by id. Returns (nil, nil) if no
// relationship with that id exists.
func (s *RelationshipStore) Get(id string) (*Relationship, error) {
	rows, err := s.db.Query(
		`SELECT `+relationshipColumns+` FROM _vibe_relationships WHERE id = ?;`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query relationship %q: %w", id, err)
	}
	defer rows.Close() //nolint:errcheck

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("failed to read relationship %q: %w", id, err)
		}
		return nil, nil
	}
	rel, err := scanRelationship(rows)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

// List returns every stored relationship, ordered by id.
func (s *RelationshipStore) List() ([]Relationship, error) {
	rows, err := s.db.Query(
		`SELECT ` + relationshipColumns + ` FROM _vibe_relationships ORDER BY id;`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list relationships: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Relationship
	for rows.Next() {
		rel, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate relationships: %w", err)
	}
	return out, nil
}

// ListForTable returns relationships where the given table is the parent or
// child, ordered by id.
func (s *RelationshipStore) ListForTable(table string) ([]Relationship, error) {
	rows, err := s.db.Query(
		`SELECT `+relationshipColumns+` FROM _vibe_relationships
		  WHERE parent_table = ? OR child_table = ?
		  ORDER BY id;`,
		table, table,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list relationships for table %q: %w", table, err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Relationship
	for rows.Next() {
		rel, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate relationships for table %q: %w", table, err)
	}
	return out, nil
}

// Delete removes a relationship definition by id.
func (s *RelationshipStore) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM _vibe_relationships WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("failed to delete relationship %q: %w", id, err)
	}
	return nil
}
