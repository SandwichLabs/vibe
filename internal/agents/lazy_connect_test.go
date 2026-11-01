package agents

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// lazyType is a non-local connection type that connects on first use, like
// MotherDuck or Postgres, without a network: it attaches an in-memory catalog,
// or fails the way a remote attach does when its spec says so.
type lazyType struct{}

func (lazyType) Name() string { return "lazytest" }
func (lazyType) Remote() bool { return true }

func (lazyType) Validate(session.Spec) error { return nil }

func (lazyType) AttachSQL(s session.Spec) ([]string, error) {
	if s.Params["fail"] != "" {
		return nil, errors.New("the remote service refused the token")
	}
	return []string{"ATTACH ':memory:' AS " + session.QuoteIdent(s.Name)}, nil
}

// newLazyHandler is a read-only Handler over a hub with two registered, not
// yet connected, connections of lazyType: "remote" (connects) and "broken"
// (its attach fails).
func newLazyHandler(t *testing.T) (*Handler, session.Session) {
	t.Helper()
	dir := t.TempDir()
	sess, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
		session.WithConnectionTypes(lazyType{}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	require.NoError(t, sess.Register(session.Spec{Name: "remote", Type: "lazytest", Params: map[string]string{}}))
	require.NoError(t, sess.Register(session.Spec{Name: "broken", Type: "lazytest", Params: map[string]string{"fail": "1"}}))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewManager(sess, engine.WithLogger(logger))
	h := NewHandler(&sessionAccess{mockAccess: &mockAccess{}, sess: sess}, eng, &SandboxConfig{Mode: ModeReadOnly, MaxQueryRows: 100}, logger)
	return h, sess
}

// The read-only runner `vibe semantics check|profile` and Studio's runs use
// connects a connection on first use when the run names it as its catalog, as
// `vibe query -c` does. A guard: this already held before #302, whose cause was
// the read-only open refusing MotherDuck (TestOpenPolicyGatedRunsReachesTheSession).
func TestRunReadOnlySQLIn_ConnectsALazyConnectionOnFirstUse(t *testing.T) {
	h, sess := newLazyHandler(t)
	ctx := context.Background()

	rs, _, err := h.RunReadOnlySQLIn(ctx, "remote", "SELECT 42 AS answer", nil, 10)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	assert.EqualValues(t, 42, rs.Rows[0][0])
	for _, c := range sess.Connections() {
		if c.Name == "remote" {
			assert.Equal(t, session.StatusOK, c.Kind, "the run connected it")
		}
	}

	// The vet a person's surface runs on a metric's SQL connects it the same way.
	require.NoError(t, h.VetReadOnlySQLIn(ctx, "remote", "SELECT 1", nil))
}

// A connection that fails to connect is reported with its own failure, not as
// "is not attached", which reads like a bug in the caller (#302).
func TestRunReadOnlySQLIn_ReportsTheConnectFailureNotNotAttached(t *testing.T) {
	h, _ := newLazyHandler(t)
	ctx := context.Background()

	_, _, err := h.RunReadOnlySQLIn(ctx, "broken", "SELECT 1", nil, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the remote service refused the token")
	assert.NotContains(t, err.Error(), "not attached")

	err = h.VetReadOnlySQLIn(ctx, "broken", "SELECT 1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the remote service refused the token")
}
