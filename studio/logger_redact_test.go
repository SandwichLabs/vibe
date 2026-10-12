package main

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStudioLoggerRedactsSecrets pins #97: whatever logger main passes in,
// the App logs through the same redacting handler as the CLI, so a secret
// value the app has seen, and credential-shaped text, never reach stderr.
func TestStudioLoggerRedactsSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VIBE_CONFIG_FILE", filepath.Join(home, "vibe-test.yaml"))

	var buf bytes.Buffer
	a := NewApp(slog.New(slog.NewJSONHandler(&buf, nil)))

	const token = "mdtok-NEEDLE-0123456789abcdef"
	// What saving a MotherDuck credential does (connections_creds.go).
	a.secrets.Redactor().Add(token)

	a.logger.Info("attach failed for "+token, "dsn", "postgres://rosa:hunter2-NEEDLE@db:5432/app")
	a.logger.With("conn", "password=pw-NEEDLE-x").Warn("probe", "error", errors.New("token "+token+" rejected"))

	out := buf.String()
	assert.NotContains(t, out, "NEEDLE", out)
	assert.Contains(t, out, "***")
	assert.Contains(t, out, "rosa", "user names stay readable in the log")
}
