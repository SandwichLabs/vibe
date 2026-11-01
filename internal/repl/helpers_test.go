package repl

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/engine"
	"github.com/SandwichLabs/assay/internal/session"
	"github.com/SandwichLabs/assay/internal/session/sessiontest"
)

// prompt is the main prompt of a test session (Workspace "ws", no colour).
const prompt = "ws ▸ D "

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fixture is a console on an in-memory hub.
type fixture struct {
	t    *testing.T
	s    *Session
	out  *syncBuffer
	sess session.Session
	eng  *engine.Manager
	db   *sql.DB
}

// newHub opens an in-memory hub with no console on it. No extension is ever
// installed or downloaded by a test.
func newHub(t *testing.T) *fixture {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{"SET autoinstall_known_extensions=false", "SET autoload_known_extensions=false"} {
		_, err = db.Exec(stmt)
		require.NoError(t, err)
	}
	sess := sessiontest.ForTests(db)
	return &fixture{t: t, out: &syncBuffer{}, sess: sess, eng: engine.NewManager(sess), db: db}
}

// open starts a console on the hub.
func (f *fixture) open(edit func(*Options)) error {
	o := Options{Engine: f.eng, Sess: f.sess, Workspace: "ws", Cols: 80, Rows: 24,
		Clock: func() time.Time { return time.Unix(0, 0) }}
	if edit != nil {
		edit(&o)
	}
	s, err := New(context.Background(), f.out, o)
	if err != nil {
		return err
	}
	f.s = s
	f.t.Cleanup(func() { _ = s.Close() })
	return nil
}

// newSession opens an in-memory hub and a console on it.
func newSession(t *testing.T, edit func(*Options)) *fixture {
	t.Helper()
	f := newHub(t)
	require.NoError(t, f.open(edit))
	return f
}

// attachFile registers a lazily attached DuckDB file called name and returns
// the path; the file holds table t(x) = 7.
func (f *fixture) lazyFile(name string) string {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), name+".duckdb")
	other, err := sql.Open("duckdb", path)
	require.NoError(f.t, err)
	_, err = other.Exec("CREATE TABLE t(x INT); INSERT INTO t VALUES (7)")
	require.NoError(f.t, err)
	require.NoError(f.t, other.Close())
	require.NoError(f.t, f.sess.Register(session.Spec{Name: name, Type: "duckdb", Path: path}))
	return path
}

func (f *fixture) typ(text string) {
	f.t.Helper()
	require.NoError(f.t, f.s.Input([]byte(text)))
}

// waitFor polls the output until it holds want.
func (f *fixture) waitFor(want string) {
	f.t.Helper()
	f.waitCount(want, 1)
}

func (f *fixture) waitCount(want string, n int) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(f.out.String(), want) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatalf("timed out waiting for %d x %q in output:\n%s", n, want, f.out.String())
}

// run types text and waits for the prompt that follows it.
func (f *fixture) run(text string) {
	f.t.Helper()
	n := strings.Count(f.out.String(), prompt) + 1
	f.typ(text)
	f.waitCount(prompt, n)
}

// hostile returns an Options editor that gives the session a root for history.
func withRoot(root string) func(*Options) { return func(o *Options) { o.Root = root } }
