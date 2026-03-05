package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Query represents a named, reusable query stored in _vibe_queries.
type Query struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	SQL        string  `json:"sql"`
	Engine     string  `json:"engine"`     // "duckdb" | "sqlite"
	Parameters []Param `json:"parameters"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// Param defines a parameter expected by a query.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}

// DB is the interface needed for database operations (avoids importing container).
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Registry provides CRUD operations for the _vibe_queries table.
type Registry struct {
	db     DB
	logger *slog.Logger
}

// NewRegistry creates a new Registry backed by the given database.
func NewRegistry(db DB, logger *slog.Logger) *Registry {
	return &Registry{
		db:     db,
		logger: logger,
	}
}

// List returns all queries in the registry ordered by title.
// Returns an empty slice (not nil) when no queries exist.
func (r *Registry) List(ctx context.Context) ([]Query, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, title, sql, engine, parameters, created_at, updated_at
		 FROM _vibe_queries ORDER BY title`)
	if err != nil {
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}
	defer rows.Close()

	queries := make([]Query, 0)
	for rows.Next() {
		var q Query
		var paramsJSON string
		if err := rows.Scan(&q.ID, &q.Title, &q.SQL, &q.Engine, &paramsJSON, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan query row: %w", err)
		}
		if err := json.Unmarshal([]byte(paramsJSON), &q.Parameters); err != nil {
			return nil, fmt.Errorf("failed to unmarshal parameters for query %q: %w", q.ID, err)
		}
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate query rows: %w", err)
	}

	return queries, nil
}

// Get retrieves a single query by ID.
func (r *Registry) Get(ctx context.Context, id string) (*Query, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, sql, engine, parameters, created_at, updated_at
		 FROM _vibe_queries WHERE id = ?`, id)

	var q Query
	var paramsJSON string
	if err := row.Scan(&q.ID, &q.Title, &q.SQL, &q.Engine, &paramsJSON, &q.CreatedAt, &q.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("query %q not found: %w", id, err)
		}
		return nil, fmt.Errorf("failed to get query %q: %w", id, err)
	}
	if err := json.Unmarshal([]byte(paramsJSON), &q.Parameters); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters for query %q: %w", id, err)
	}

	return &q, nil
}

// Create inserts a new query into the registry.
// It sets CreatedAt and UpdatedAt to the current time.
func (r *Registry) Create(ctx context.Context, q *Query) error {
	paramsJSON, err := json.Marshal(q.Parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters for query %q: %w", q.ID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	q.CreatedAt = now
	q.UpdatedAt = now

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO _vibe_queries (id, title, sql, engine, parameters, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.Title, q.SQL, q.Engine, string(paramsJSON), q.CreatedAt, q.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create query %q: %w", q.ID, err)
	}

	r.logger.Info("query created", "id", q.ID, "title", q.Title)
	return nil
}

// Update modifies an existing query in the registry.
// It sets UpdatedAt to the current time.
func (r *Registry) Update(ctx context.Context, q *Query) error {
	paramsJSON, err := json.Marshal(q.Parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters for query %q: %w", q.ID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	q.UpdatedAt = now

	result, err := r.db.ExecContext(ctx,
		`UPDATE _vibe_queries SET title = ?, sql = ?, engine = ?, parameters = ?, updated_at = ?
		 WHERE id = ?`,
		q.Title, q.SQL, q.Engine, string(paramsJSON), q.UpdatedAt, q.ID)
	if err != nil {
		return fmt.Errorf("failed to update query %q: %w", q.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected for query %q: %w", q.ID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("query %q not found", q.ID)
	}

	r.logger.Info("query updated", "id", q.ID, "title", q.Title)
	return nil
}

// Delete removes a query from the registry by ID.
func (r *Registry) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM _vibe_queries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete query %q: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected for query %q: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("query %q not found", id)
	}

	r.logger.Info("query deleted", "id", id)
	return nil
}
