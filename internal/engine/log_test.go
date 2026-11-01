package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/session/sessiontest"
)

func TestRunLogsNoStatementText(t *testing.T) {
	db := openTestDB(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := NewManager(sessiontest.ForTests(db), WithLogger(log))
	const q = "SELECT 'hunter2-literal' AS marker"
	_, err := m.Execute(context.Background(), q, nil)
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "hunter2-literal")
	assert.NotContains(t, buf.String(), "SELECT")
	assert.Contains(t, buf.String(), "query_sha256="+QueryDigest(q))
	assert.Contains(t, buf.String(), "query_bytes=")
}

func TestErrorForLogDropsTheStatementEcho(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("SELECT 'topsecret-literal' FROM nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "topsecret-literal", "DuckDB echoes the statement")
	got := ErrorForLog(err)
	assert.NotContains(t, got, "topsecret-literal")
	assert.NotContains(t, got, "LINE 1")
	assert.Empty(t, ErrorForLog(nil))
}

func TestErrorForLogRedactsQuotedRuns(t *testing.T) {
	for in, want := range map[string]string{
		`Parser Error: syntax error at or near "'hunter2'"`:              `Parser Error: syntax error at or near "..."`,
		`Parser Error: syntax error at or near "$$hunter2 more text"`:    `Parser Error: syntax error at or near "..."`,
		`Conversion Error: Could not convert string 'hunter2' to INT32`:  `Conversion Error: Could not convert string '...' to INT32`,
		`bad value 'it''s hunter2' and "a ""b"" hunter2" end`:            `bad value '...' and "..." end`,
		`syntax error at or near $t$hunter2 more; text`:                  `syntax error at or near $...$`,
		`Parameter $1 is not bound`:                                      `Parameter $1 is not bound`,
		`unterminated 'hunter2 but nothing closes it`:                    `unterminated '...'`,
		"Catalog Error: Table with name nope does not exist!\nLINE 1: x": `Catalog Error: Table with name nope does not exist!`,
	} {
		got := ErrorForLog(errors.New(in))
		assert.Equal(t, want, got, in)
		assert.NotContains(t, got, "hunter2")
	}
}
