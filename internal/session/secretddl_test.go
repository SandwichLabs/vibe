package session

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ddlNeedle = "NEEDLE-bearer-9f3a'quote"

func TestCreateTemporarySecretHTTP(t *testing.T) {
	var logs bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newTestSession(t, WithLogger(lg))
	ctx := context.Background()

	require.NoError(t, s.CreateTemporarySecret(ctx, "web", "http", []SecretParam{
		{Key: "BEARER_TOKEN", Value: ddlNeedle},
	}))
	// The secret exists, and replacing it works.
	var n int
	rows, err := s.Query(ctx, "SELECT count(*) FROM duckdb_secrets() WHERE name = 'web'")
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&n))
	require.NoError(t, rows.Close())
	assert.Equal(t, 1, n)
	require.NoError(t, s.CreateTemporarySecret(ctx, "web", "http", []SecretParam{{Key: "BEARER_TOKEN", Value: ddlNeedle + "2"}}))

	require.NoError(t, s.DropSecret(ctx, "web"))
	require.NoError(t, s.DropSecret(ctx, "web"), "dropping a missing secret is fine")

	assert.NotContains(t, logs.String(), "NEEDLE", "secret DDL is never logged")
}

func TestCreateTemporarySecretErrorsAreScrubbed(t *testing.T) {
	var logs bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newTestSession(t, WithLogger(lg))
	ctx := context.Background()

	// An unknown secret type fails inside DuckDB, whose message can quote the
	// statement.
	err := s.CreateTemporarySecret(ctx, "bad", "nosuchtype", []SecretParam{{Key: "PASSWORD", Value: ddlNeedle}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "NEEDLE")
	assert.NotContains(t, logs.String(), "NEEDLE")
	// Later errors from the same session scrub the value too, wherever it shows.
	assert.NotContains(t, s.Redact("boom "+ddlNeedle), "NEEDLE")
}

func TestSecretSQLQuotingAndValidation(t *testing.T) {
	stmt, vals, err := secretSQL("pg", "postgres", []SecretParam{
		{Key: "HOST", Value: "h"}, {Key: "PORT", Value: 5432}, {Key: "PASSWORD", Value: "it's"}, {Key: "USE_SSL", Value: true},
	})
	require.NoError(t, err)
	assert.Equal(t, `CREATE OR REPLACE TEMPORARY SECRET "pg" (TYPE postgres, HOST 'h', PORT 5432, PASSWORD 'it''s', USE_SSL true)`, stmt)
	assert.Equal(t, []string{"it's"}, vals, "only secret-bearing options are registered with the redactor")

	_, _, err = secretSQL("pg", "postgres) ; DROP", nil)
	assert.Error(t, err)
	_, _, err = secretSQL("pg", "http", []SecretParam{{Key: "X'; DROP", Value: "v"}})
	assert.Error(t, err)
	_, _, err = secretSQL("Bad Name", "http", nil)
	assert.Error(t, err)
	_, _, err = secretSQL("ok", "http", []SecretParam{{Key: "A", Value: 1.5}})
	assert.Error(t, err)
}

func TestQueryErrorsKeepCauseButScrubText(t *testing.T) {
	s := newTestSession(t)
	s.Redactor().Add("pw-NEEDLE-1")
	ctx := context.Background()

	_, err := s.Exec(ctx, `SELECT * FROM "pw-NEEDLE-1"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist", "the useful part of the message survives")
	assert.NotContains(t, err.Error(), "NEEDLE")

	rows, err := s.Query(ctx, `SELECT * FROM "pw-NEEDLE-1"`)
	if rows != nil {
		_ = rows.Close()
	}
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "NEEDLE")

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Exec(cctx, "SELECT 1")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "wrapping keeps the cause")
}

func TestSecretOptionsDoNotMaskOrdinaryText(t *testing.T) {
	s := newTestSession(t)
	ctx := context.Background()
	// A TYPE http secret with a secret-bearing option, plus plain-looking text.
	require.NoError(t, s.CreateTemporarySecret(ctx, "web2", "http", []SecretParam{{Key: "BEARER_TOKEN", Value: "tok-NEEDLE-long"}}))
	msg := "Unable to connect to Postgres at user='rosa' password='x' host='127.0.0.1' db=postgres region=us-east-1"
	got := s.Redact(msg)
	assert.Contains(t, got, "user='rosa'")
	assert.Contains(t, got, "127.0.0.1")
	assert.Contains(t, got, "postgres")
	assert.Contains(t, got, "us-east-1")
	assert.NotContains(t, got, "password='x'")
}

func TestSecretErrorIsAFixedMessage(t *testing.T) {
	s := newTestSession(t)
	ctx := context.Background()
	for _, p := range [][]SecretParam{
		{{Key: "BEARER_TOKEN", Value: "Ab1"}, {Key: "NO_SUCH_OPTION", Value: "Zz9"}},
		{{Key: "EXTRA_HTTP_HEADERS", Value: "Ab1"}},
		{{Key: "BEARER_TOKEN", Value: "Ab1"}, {Key: "EXTRA_HTTP_HEADERS", Value: 5}},
	} {
		err := s.CreateTemporarySecret(ctx, "bad", "http", p)
		if err == nil {
			continue
		}
		for _, leak := range []string{"Ab1", "Zz9", "***'"} {
			assert.NotContains(t, err.Error(), leak)
		}
		assert.NotContains(t, err.Error(), "'")
		assert.Contains(t, err.Error(), "values hidden")
		assert.Contains(t, err.Error(), p[0].Key, "names the options")
	}
	err := secretError("x", errors.New("Invalid Input Error: Type VARCHAR with value 'Ab1'Ab1't be cast"), []SecretParam{{Key: "PASSWORD", Value: "Ab1"}})
	assert.Equal(t, `failed to create secret "x": Invalid Input Error: Type VARCHAR with value (values hidden; options: PASSWORD)`, err)
	err = secretError("x", errors.New("Binder Error: bad Ab1 here"), []SecretParam{{Key: "PASSWORD", Value: "Ab1"}})
	assert.NotContains(t, err, "Ab1")
	assert.True(t, (SecretParam{Key: "EXTRA_HTTP_HEADERS"}).bearsSecret())
}
