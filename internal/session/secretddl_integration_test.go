//go:build integration

package session

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CreateTemporarySecret followed by an ATTACH that fails (wrong password
// against a real Postgres) must leave the password in no error, log line or
// session state (#114). Postgres echoes password='...' in its connect error.
func TestSecretThenFailingAttachLeaksNothing(t *testing.T) {
	pg := startPG(t)
	var logs bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newTestSession(t, WithLogger(lg), WithExtensionDirectory(filepath.Join(os.TempDir(), "vibe-spike-ext")))
	ctx := context.Background()
	_, err := s.Exec(ctx, "LOAD postgres")
	require.NoError(t, err)
	port, err := strconv.Atoi(pg.port)
	require.NoError(t, err)

	for i, pw := range []string{"WRONG-NEEDLE-pw'q", "N3x", "WRONG NEEDLE with space", `WRONG\NEEDLE"bs`} {
		name := fmt.Sprintf("pgbad%d", i)
		require.NoError(t, s.CreateTemporarySecret(ctx, name, "postgres", []SecretParam{
			{Key: "HOST", Value: "127.0.0.1"}, {Key: "PORT", Value: port}, {Key: "DATABASE", Value: "postgres"},
			{Key: "USER", Value: "postgres"}, {Key: "PASSWORD", Value: pw},
		}))
		_, err := s.Exec(ctx, "ATTACH '' AS "+name+" (TYPE postgres, SECRET "+name+", READ_ONLY)")
		require.Error(t, err, "the password is wrong")
		assert.NotContains(t, err.Error(), pw, "pw %q in the error", pw)
		assert.Contains(t, err.Error(), "postgres", "the useful part survives")
		for _, st := range s.Connections() {
			assert.NotContains(t, st.Message, pw)
		}
		assert.NotContains(t, logs.String(), pw)
	}
	// Ordinary option values stay readable: only the password is masked.
	assert.NotContains(t, s.Redact("host 127.0.0.1 db postgres"), "***")
}
