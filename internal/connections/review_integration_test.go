//go:build integration

package connections_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func rawSession(t *testing.T, specs ...session.Spec) session.Session {
	t.Helper()
	s, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(extDir(t)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	for _, sp := range specs {
		require.NoError(t, s.Register(sp))
	}
	return s
}

func pgCred(context.Context) (secrets.Credential, error) {
	return secrets.Credential{User: "postgres", Password: pgPassword}, nil
}

// libpq keywords in the database field stay inside dbname: DuckDB quotes the
// value, so the field cannot add a host= (and with it redirect the password).
func TestDatabaseFieldCannotInjectConninfo(t *testing.T) {
	pg := startPG(t)
	port, _ := strconv.Atoi(pg.port)
	ctx := context.Background()
	for _, db := range []string{"nonexistent_db dbname=postgres", "nonexistent_db' dbname='postgres", `x\' host=evil.example`} {
		d := connections.Def{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: db, Options: map[string]string{"sslmode": "disable"}}
		sp := d.Spec()
		sp.Credential = pgCred
		s := rawSession(t, sp)
		require.NoError(t, s.InstallExtension(ctx, "postgres"))
		err := s.Connect(ctx, "pg")
		require.Error(t, err, db)
		assert.Contains(t, err.Error(), "does not exist", "the whole value was taken as one database name: %q", db)
		assert.NotContains(t, err.Error(), "evil.example connection", "never dialled another host")
		_ = s.Close()
	}
}

// fakePG accepts connections, asks for a cleartext password and reports it.
func fakePG(t *testing.T) (int, <-chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				for {
					var hdr [8]byte
					if _, err := io.ReadFull(c, hdr[:]); err != nil {
						return
					}
					n := binary.BigEndian.Uint32(hdr[:4])
					code := binary.BigEndian.Uint32(hdr[4:])
					rest := make([]byte, n-8)
					if _, err := io.ReadFull(c, rest); err != nil {
						return
					}
					if code == 80877103 || code == 80877104 {
						_, _ = c.Write([]byte{'N'})
						continue
					}
					_, _ = c.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 3})
					var th [5]byte
					if _, err := io.ReadFull(c, th[:]); err != nil {
						return
					}
					pl := make([]byte, binary.BigEndian.Uint32(th[1:])-4)
					_, _ = io.ReadFull(c, pl)
					ch <- strings.TrimRight(string(pl), "\x00")
					return
				}
			}(c)
		}
	}()
	addr, _ := ln.Addr().(*net.TCPAddr)
	return addr.Port, ch
}

func openWith(t *testing.T, ws workspace.Workspace, ext string) session.Session {
	t.Helper()
	sess, _, err := workspace.OpenSession(context.Background(), ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(ext)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	require.NoError(t, sess.InstallExtension(context.Background(), "postgres"))
	return sess
}

func passwordReceived(ch <-chan string) (string, bool) {
	select {
	case pw := <-ch:
		return pw, true
	case <-time.After(2 * time.Second):
		return "", false
	}
}

// A committed vibe.yaml must not be able to choose env: credentials and a host
// of its choosing: a statement that merely names the connection would send the
// person's environment password there.
func TestSharedEnvHintNeverSendsThePasswordToTheCommittedHost(t *testing.T) {
	port, got := fakePG(t)
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", Credentials: "env:PG_PROD"}))
	t.Setenv("PG_PROD_USER", "alice")
	t.Setenv("PG_PROD_PASSWORD", "ExfilNeedle_777")
	// Resolve from the real environment in this test.
	r := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()))
	root := ws.Root()
	_ = ws.Close()
	ws2, err := workspace.Open(root, workspace.WithConfigDir(t.TempDir()),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	defer ws2.Close() //nolint:errcheck
	sess := openWith(t, ws2, extDir(t))

	err = sess.ConnectReferenced(ctx, "", "DELETE FROM pg.public.t")
	require.Error(t, err)
	assert.Equal(t, session.StatusNeedsCredentials, connStatus(sess, "pg").Kind)
	pw, sent := passwordReceived(got)
	assert.False(t, sent, "the server received a password: %q", pw)
}

// Even a sign-in the person really chose is withheld once a pull points the
// connection somewhere else.
func TestChosenEnvSignInIsWithheldWhenTheHostChanges(t *testing.T) {
	port, got := fakePG(t)
	ctx := context.Background()
	t.Setenv("PGX_USER", "alice")
	t.Setenv("PGX_PASSWORD", "ExfilNeedle_888")
	res, err := workspace.Init(t.TempDir() + "/acme")
	require.NoError(t, err)
	root := res.Workspace.Root()
	_ = res.Workspace.Close()
	r := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()))
	ws, err := workspace.Open(root, workspace.WithConfigDir(t.TempDir()),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	good := workspace.ConnectionDef{Name: "pg", Type: "postgres", Host: "db.trusted.example", Port: 5432, Database: "postgres"}
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, good))
	require.NoError(t, ws.SetSignIn("pg", "env:PGX"))
	// A pull re-points it at the attacker's listener.
	evil := good
	evil.Host, evil.Port = "127.0.0.1", port
	require.NoError(t, ws.UpdateConnection(workspace.ScopeShared, evil))
	sess := openWith(t, ws, extDir(t))
	require.Error(t, sess.ConnectReferenced(ctx, "", "SELECT * FROM pg.public.t"))
	assert.Equal(t, session.StatusNeedsCredentials, connStatus(sess, "pg").Kind)
	pw, sent := passwordReceived(got)
	assert.False(t, sent, "the server received a password: %q", pw)
}

// Eight queries race to be the first user of a connection.
func TestConcurrentFirstUseOfAConnection(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "create table people(id int); insert into people values (1),(2)")
	port, _ := strconv.Atoi(pg.port)
	ctx := context.Background()
	d := connections.Def{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", ReadOnly: true, Options: map[string]string{"sslmode": "disable"}}
	sp := d.Spec()
	sp.Credential = pgCred
	s := rawSession(t, sp)
	require.NoError(t, s.InstallExtension(ctx, "postgres"))
	eng := engine.NewManager(s)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = eng.Execute(ctx, "SELECT count(*) FROM pg.public.people", nil)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		assert.NoError(t, e, "query %d", i)
	}
	assert.Equal(t, session.StatusOK, connStatus(s, "pg").Kind)
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name='vibe_pg'").Scan(&n))
	assert.Equal(t, 1, n)
}

// Unrelated local statements are not held up by a pending connection that
// shares a word with them.
func TestUnrelatedStatementsRunWhilstAConnectionIsPending(t *testing.T) {
	ctx := context.Background()
	d := connections.Def{Name: "sales", Type: "postgres", Host: "127.0.0.1", Port: 1, Database: "postgres"}
	s := rawSession(t, d.Spec()) // no credential: needs-credentials
	eng := engine.NewManager(s)
	for _, q := range []string{"CREATE TABLE sales AS SELECT 1 AS x", "SELECT 42 AS sales", "SELECT 'sales.t' AS x -- sales.t"} {
		_, err := eng.Execute(ctx, q, nil)
		assert.NoError(t, err, q)
	}
	_, err := eng.Execute(ctx, "SELECT * FROM sales.public.orders", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No credentials", "the connection's own message")
}

// A failed connection is not dialled again for every statement.
func TestFailedConnectionIsNotRetriedOnEveryStatement(t *testing.T) {
	port, got := fakePG(t)
	ctx := context.Background()
	d := connections.Def{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres"}
	sp := d.Spec()
	sp.Credential = pgCred
	s := rawSession(t, sp)
	require.NoError(t, s.InstallExtension(ctx, "postgres"))
	for i := 0; i < 4; i++ {
		_ = s.ConnectReferenced(ctx, "", "SELECT * FROM pg.public.t")
	}
	n := 0
	for {
		if _, ok := passwordReceived(got); !ok {
			break
		}
		n++
	}
	assert.Equal(t, 1, n, "one attempt, then the recorded failure")
}
