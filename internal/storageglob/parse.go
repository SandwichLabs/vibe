package storageglob

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	// Registers the "duckdb" driver the parser connection opens.
	_ "github.com/duckdb/duckdb-go/v2"
)

// Parser parses one piece of SQL text with DuckDB's own parser and returns the
// SELECT it holds as a generic tree, or an error when it is not exactly one
// SELECT statement.
type Parser interface {
	Parse(ctx context.Context, text string) (map[string]any, error)
}

// DefaultParser is the process-wide hardened parser: json_serialize_sql on an
// in-memory DuckDB with external access off and the configuration locked. It
// only parses; it never binds, so it touches no file, network or secret.
func DefaultParser() Parser { return defaultParser{} }

type defaultParser struct{}

var (
	parserOnce sync.Once
	parserDB   *sql.DB
	parserErr  error
)

func openParser() (*sql.DB, error) {
	parserOnce.Do(func() {
		dsn := "?enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false" +
			"&allow_community_extensions=false&allow_persistent_secrets=false&lock_configuration=true"
		db, err := sql.Open("duckdb", dsn)
		if err != nil {
			parserErr = err
			return
		}
		db.SetMaxOpenConns(1)
		var v string
		if err := db.QueryRow("SELECT current_setting('enable_external_access')").Scan(&v); err != nil || !strings.EqualFold(v, "false") {
			_ = db.Close()
			parserErr = fmt.Errorf("the SQL parser connection is not hardened (enable_external_access=%q)", v)
			return
		}
		parserDB = db
	})
	return parserDB, parserErr
}

// ErrNotSelect is what Parse returns for text that is not one SELECT.
var ErrNotSelect = errors.New("not a single SELECT statement")

func (defaultParser) Parse(ctx context.Context, text string) (map[string]any, error) {
	db, err := openParser()
	if err != nil {
		return nil, fmt.Errorf("parser connection unavailable: %w", err)
	}
	var out string
	if err := db.QueryRowContext(ctx, "SELECT CAST(json_serialize_sql(?::VARCHAR) AS VARCHAR)", text).Scan(&out); err != nil {
		return nil, fmt.Errorf("json_serialize_sql failed: %w", err)
	}
	var doc struct {
		Error      bool             `json:"error"`
		Statements []map[string]any `json:"statements"`
	}
	// Numbers stay json.Number: a parser location may be 2^64-1 ("unknown").
	dec := json.NewDecoder(bytes.NewReader([]byte(out)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to read the parser output: %w", err)
	}
	if doc.Error || len(doc.Statements) != 1 {
		return nil, ErrNotSelect
	}
	return doc.Statements[0], nil
}
