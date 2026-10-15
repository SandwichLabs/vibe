package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

// stubType is a credentialed type that needs no network: its "remote" is an
// in-memory DuckDB attach and its secret is a core http secret.
type stubType struct {
	ext      string
	catalogs bool
}

func (stubType) Name() string              { return "stub" }
func (stubType) Remote() bool              { return true }
func (stubType) Validate(Spec) error       { return nil }
func (s stubType) Extensions() []string    { return []string{s.ext} }
func (stubType) NeedsCredential(Spec) bool { return true }
func (s stubType) Catalogless() bool       { return !s.catalogs }
func (stubType) SecretSteps(sp Spec, c secrets.Credential) ([]SecretStep, error) {
	return []SecretStep{{Name: "stub_" + sp.Name, Type: "http", Params: []SecretParam{
		{Key: "BEARER_TOKEN", Value: c.Token, Secret: true},
	}}}, nil
}
func (s stubType) AttachSQL(sp Spec) ([]string, error) {
	if !s.catalogs {
		return []string{}, nil
	}
	return []string{"ATTACH ':memory:' AS " + QuoteIdent(sp.Name)}, nil
}

func stubSession(t *testing.T, st stubType) Session {
	return newTestSession(t, WithConnectionTypes(st))
}

func credOK(tok string) CredentialFunc {
	return func(context.Context) (secrets.Credential, error) { return secrets.Credential{Token: tok}, nil }
}

func status(t *testing.T, s Session, name string) Status {
	t.Helper()
	for _, st := range s.Connections() {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no status for %s", name)
	return Status{}
}

func TestCredentialedConnect(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "r", Type: "stub", Credential: credOK("tok_needle_123")}))
	assert.Equal(t, StatusNotConnected, status(t, s, "r").Kind, "registering dials nothing")
	require.NoError(t, s.Connect(ctx, "r"))
	assert.Equal(t, StatusOK, status(t, s, "r").Kind)
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_r'").Scan(&n))
	assert.Equal(t, 1, n)

	// The token is registered with the redactor, so an echoed error is scrubbed.
	assert.NotContains(t, s.Redact("failed with tok_needle_123 in it"), "tok_needle_123")

	// Detach drops the secret it created.
	require.NoError(t, s.Detach(ctx, "r"))
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_r'").Scan(&n))
	assert.Equal(t, 0, n)
}

func TestNeedsCredentialsIsDecidedBeforeDialling(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	for name, c := range map[string]struct {
		fn   CredentialFunc
		want string
	}{
		"none": {nil, "No credentials"},
		"notset": {func(context.Context) (secrets.Credential, error) {
			return secrets.Credential{}, &secrets.ErrNeedsCredentials{Method: "keychain", Reason: "not set", Ref: secrets.WorkspaceRef("w", "x")}
		}, "no credentials for x"},
		"changed": {func(context.Context) (secrets.Credential, error) {
			return secrets.Credential{}, &secrets.ErrFingerprintChanged{Old: "a:1", New: "b:2"}
		}, "server changed"},
		"changed_unnamed": {func(context.Context) (secrets.Credential, error) {
			return secrets.Credential{}, &secrets.ErrFingerprintChanged{New: "b:2"}
		}, "server changed"},
		"nokeys": {func(context.Context) (secrets.Credential, error) { return secrets.Credential{}, secrets.ErrUnavailable }, "unavailable"},
	} {
		require.NoError(t, s.Register(Spec{Name: name, Type: "stub", Credential: c.fn}))
		err := s.Connect(ctx, name)
		require.Error(t, err, name)
		st := status(t, s, name)
		assert.Equal(t, StatusNeedsCredentials, st.Kind, name)
		assert.Contains(t, strings.ToLower(st.Message), strings.ToLower(c.want), name)
		if name == "changed" {
			require.NotNil(t, st.ServerChanged, "the changed server is typed data")
			assert.Equal(t, ServerChange{Old: "a:1", New: "b:2"}, *st.ServerChanged)
		} else {
			assert.Nil(t, st.ServerChanged, name)
		}
		// Nothing was attached.
		var n int
		require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name = $1", name).Scan(&n))
		assert.Equal(t, 0, n, name)
	}
}

func TestExtensionMissingOutranksNeedsCredentials(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "definitely_not_an_extension", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "e", Type: "stub"})) // no credential either
	s.Preflight(ctx, "e")
	st := status(t, s, "e")
	assert.Equal(t, StatusExtensionMissing, st.Kind)
	assert.Contains(t, st.Message, "Install")
	err := s.Connect(ctx, "e")
	require.Error(t, err)
	assert.Equal(t, StatusExtensionMissing, status(t, s, "e").Kind)
	// Nothing is installed behind the person's back.
	require.Error(t, s.InstallExtension(ctx, "httpfs"), "an extension no registered type needs is refused")
}

func TestPreflightReportsNeedsCredentialsWithoutDialling(t *testing.T) {
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	calls := 0
	require.NoError(t, s.Register(Spec{Name: "p", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		calls++
		return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "p")}
	}}))
	s.Preflight(context.Background(), "p")
	assert.Equal(t, StatusNeedsCredentials, status(t, s, "p").Kind)
	assert.Equal(t, 1, calls)
	// After the credential appears, preflight clears the status.
	require.NoError(t, s.Register(Spec{Name: "q", Type: "stub", Credential: credOK("t")}))
	s.Preflight(context.Background(), "q")
	assert.Equal(t, StatusNotConnected, status(t, s, "q").Kind)
}

func TestCataloglessConnectionCreatesOnlyASecret(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: false})
	require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: credOK("k")}))
	require.NoError(t, s.Connect(ctx, "store"))
	assert.Equal(t, StatusOK, status(t, s, "store").Kind)
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name = 'store'").Scan(&n))
	assert.Equal(t, 0, n, "no catalog attached")
	_, err := s.Conn(ctx, WithCatalog("store"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage")
	require.NoError(t, s.Detach(ctx, "store"))
}

func TestConnectReferencedAndResolveAreLazy(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "pg", Type: "stub", Credential: credOK("a")}))
	require.NoError(t, s.Register(Spec{Name: "other", Type: "stub", Credential: credOK("b")}))

	// A statement that does not name them dials nothing.
	require.NoError(t, s.ConnectReferenced(ctx, "", "SELECT 1 AS pg_total, 'pg' AS x -- pg.t"))
	assert.Equal(t, StatusNotConnected, status(t, s, "pg").Kind, "names in strings and comments do not count")
	// Naming one dials exactly that one.
	require.NoError(t, s.ConnectReferenced(ctx, "", `SELECT * FROM pg.main.orders JOIN "x" ON true`))
	assert.Equal(t, StatusOK, status(t, s, "pg").Kind)
	assert.Equal(t, StatusNotConnected, status(t, s, "other").Kind)
	// The default-catalog argument counts.
	require.NoError(t, s.ConnectReferenced(ctx, "other", "SELECT 1"))
	assert.Equal(t, StatusOK, status(t, s, "other").Kind)

	// Resolve connects lazily too.
	s2 := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s2.Register(Spec{Name: "lazy", Type: "stub", Credential: credOK("c")}))
	ref, err := s2.Resolve(ctx, TableRef{Schema: "lazy", Name: "t"})
	require.NoError(t, err)
	assert.Equal(t, "lazy", ref.Catalog)
	assert.Equal(t, StatusOK, status(t, s2, "lazy").Kind)
}

func TestConnectReferencedSurfacesTheConnectionsOwnProblem(t *testing.T) {
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "down", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "down")}
	}}))
	err := s.ConnectReferenced(context.Background(), "", "SELECT * FROM down.main.t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}

func TestQualifiers(t *testing.T) {
	w := qualifiers(`SELECT a.b, "Q""x".t, sales FROM pg.public.x -- not_this.x
	/* nor_this.y */ WHERE s = 'nor_that.z' AND é_1.c > 1 AND $$dollar.q$$ = $tag$tagged.q$tag$ AND 1.5 > 1`)
	for _, want := range []string{"a", `q"x`, "pg", "é_1"} {
		assert.True(t, w[want], want)
	}
	for _, no := range []string{"public", "sales", "b", "x", "not_this", "nor_this", "nor_that", "dollar", "tagged", "q", "c"} {
		assert.False(t, w[no], no)
	}
}

// A pending connection that merely shares a word with an unrelated statement
// is not dialled, and a failing one never fails a local statement.
func TestSharedWordsDoNotDialAPendingConnection(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	calls := 0
	require.NoError(t, s.Register(Spec{Name: "sales", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		calls++
		return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "sales")}
	}}))
	for _, q := range []string{
		"CREATE TABLE sales AS SELECT 1 AS x", "SELECT 42 AS sales", "SELECT 'sales.x' AS x -- sales.y",
		"SELECT $$sales.x$$ AS x", "SELECT sales FROM (SELECT 1 AS sales)",
	} {
		require.NoError(t, s.ConnectReferenced(ctx, "", q), q)
	}
	assert.Zero(t, calls)
	// Only a qualified use dials; its failure is reported to the caller.
	require.Error(t, s.ConnectReferenced(ctx, "", "SELECT * FROM sales.main.t"))
	assert.Equal(t, 1, calls)
}

func TestImplicitConnectBacksOffAfterAFailureAndExplicitConnectResets(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	calls := 0
	var fail atomic.Bool
	fail.Store(true)
	require.NoError(t, s.Register(Spec{Name: "r", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		calls++
		if fail.Load() {
			return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "r")}
		}
		return secrets.Credential{Token: "t"}, nil
	}}))
	for i := 0; i < 5; i++ {
		require.Error(t, s.ConnectReferenced(ctx, "", "SELECT * FROM r.main.t"))
	}
	assert.Equal(t, 1, calls, "a failed connection is not retried on every statement")
	fail.Store(false)
	require.Error(t, s.ConnectReferenced(ctx, "", "SELECT * FROM r.main.t"), "still inside the backoff window")
	require.NoError(t, s.Connect(ctx, "r"), "an explicit Connect always tries")
	assert.Equal(t, StatusOK, status(t, s, "r").Kind)
}

func TestPreflightResetsTheBackoff(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	var fail atomic.Bool
	fail.Store(true)
	require.NoError(t, s.Register(Spec{Name: "r", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		if fail.Load() {
			return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "r")}
		}
		return secrets.Credential{Token: "t"}, nil
	}}))
	require.Error(t, s.ConnectReferenced(ctx, "", "SELECT * FROM r.main.t"))
	fail.Store(false) // the person signed in
	s.Preflight(ctx, "r")
	require.NoError(t, s.ConnectReferenced(ctx, "", "SELECT * FROM r.main.t"))
	assert.Equal(t, StatusOK, status(t, s, "r").Kind)
}

// Eight goroutines hit a not-yet-connected connection together: every one of
// them succeeds, the secret survives, and the status stays ok.
func TestConcurrentFirstUse(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "r", Type: "stub", Credential: credOK("tok")}))
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs[i] = s.ConnectReferenced(ctx, "", "SELECT * FROM r.main.t")
			} else {
				_, errs[i] = s.Resolve(ctx, TableRef{Schema: "r", Name: "t"})
			}
			_ = s.Connections()
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		assert.NoError(t, e, "goroutine %d", i)
	}
	assert.Equal(t, StatusOK, status(t, s, "r").Kind)
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_r'").Scan(&n))
	assert.Equal(t, 1, n)
}

// A failed attach never disturbs a connection that is already attached.
func TestFailedRetryDoesNotDropAnAttachedConnectionsSecret(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: true})
	require.NoError(t, s.Register(Spec{Name: "r", Type: "stub", Credential: credOK("tok")}))
	require.NoError(t, s.Connect(ctx, "r"))
	h, ok := s.(*hub)
	require.True(t, ok)
	require.NoError(t, h.connectLocked(ctx, "r"), "already attached: a no-op")
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_r'").Scan(&n))
	assert.Equal(t, 1, n)
	assert.Equal(t, StatusOK, status(t, s, "r").Kind)
}

func TestStorageConnectionsComeUpAtPreflightAndFollowCredentialChanges(t *testing.T) {
	ctx := context.Background()
	s := stubSession(t, stubType{ext: "json", catalogs: false})
	var tok atomic.Value
	tok.Store("one")
	have := atomic.Bool{}
	have.Store(true)
	require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		if !have.Load() {
			return secrets.Credential{}, &secrets.ErrNeedsCredentials{Ref: secrets.WorkspaceRef("w", "store")}
		}
		v, _ := tok.Load().(string)
		return secrets.Credential{Token: v}, nil
	}}))
	s.Preflight(ctx, "store") // creating a secret dials nothing
	assert.Equal(t, StatusOK, status(t, s, "store").Kind)
	secretCount := func() int {
		var n int
		require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_store'").Scan(&n))
		return n
	}
	assert.Equal(t, 1, secretCount())
	tok.Store("two")
	s.Preflight(ctx, "store")
	assert.Equal(t, 1, secretCount(), "replaced, not duplicated")
	have.Store(false)
	s.Preflight(ctx, "store")
	assert.Equal(t, StatusNeedsCredentials, status(t, s, "store").Kind)
	assert.Equal(t, 0, secretCount(), "a cleared credential drops the secret")
}

func TestClassifyAuthFailureAsNeedsCredentials(t *testing.T) {
	k, _, _ := classifyAttachError(errors.New(`FATAL: password authentication failed for user "x"`), secrets.RedactPatterns)
	assert.Equal(t, StatusNeedsCredentials, k)
	k, _, _ = classifyAttachError(errors.New("connection refused"), secrets.RedactPatterns)
	assert.Equal(t, StatusError, k)
}

// --- MotherDuck hub-open handling (no network, no token) ---

func TestMotherDuckTokenScrubsAmbientEnvBeforeAnyLoad(t *testing.T) {
	t.Setenv("MOTHERDUCK_TOKEN", "ambient_needle_1")
	t.Setenv("motherduck_token", "ambient_needle_2")
	s := newTestSession(t, WithMotherDuckToken("hub_tok_needle"))
	_, ok1 := os.LookupEnv("MOTHERDUCK_TOKEN")
	_, ok2 := os.LookupEnv("motherduck_token")
	assert.False(t, ok1 || ok2, "ambient MotherDuck variables are gone once the hub opens")
	// The extension is not installed in this empty extension directory, so the
	// token is held back, never silently dropped and never attached.
	h, ok := s.(*hub)
	require.True(t, ok)
	require.NotNil(t, h.md)
	assert.False(t, h.md.loaded)
	assert.NotContains(t, s.Redact("x hub_tok_needle y"), "hub_tok_needle", "the token is known to the redactor")
}

func TestScrubDoesNotRunWithoutAMotherDuckToken(t *testing.T) {
	t.Setenv("MOTHERDUCK_TOKEN", "left_alone")
	_ = newTestSession(t)
	assert.Equal(t, "left_alone", os.Getenv("MOTHERDUCK_TOKEN"))
}

// mdStub plays MotherDuck: hub-token type whose attach would dial.
type mdStub struct{ stubType }

func (mdStub) Name() string       { return "mdstub" }
func (mdStub) UsesHubToken() bool { return true }
func (mdStub) AttachSQL(Spec) ([]string, error) {
	return nil, errors.New("must never be reached without a hub token")
}

func TestMotherDuckNeverAttachesWithoutTheHubToken(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t, WithConnectionTypes(mdStub{stubType{ext: "json", catalogs: true}}))
	require.NoError(t, s.Register(Spec{Name: "md", Type: "mdstub", Credential: credOK("some_token")}))
	err := s.Connect(ctx, "md")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "must never be reached", "the guard stops it before AttachSQL")
	assert.Equal(t, StatusError, status(t, s, "md").Kind)
	relaunch, why := s.RelaunchRequired()
	assert.True(t, relaunch)
	assert.Contains(t, why, "Reopen")

	// No credential at all: needs-credentials, still no attach.
	require.NoError(t, s.Register(Spec{Name: "md2", Type: "mdstub"}))
	require.Error(t, s.Connect(ctx, "md2"))
	assert.Equal(t, StatusNeedsCredentials, status(t, s, "md2").Kind)
}

func TestTokenIDDistinguishesAccounts(t *testing.T) {
	assert.Equal(t, TokenID("a"), TokenID("a"))
	assert.NotEqual(t, TokenID("a"), TokenID("b"))
	assert.NotContains(t, TokenID("secret_token"), "secret")
}

// specExtType needs the extensions its spec names (option "exts", comma
// separated), the way a DuckLake needs sqlite for a SQLite catalog.
type specExtType struct{ stubType }

func (specExtType) SpecExtensions(sp Spec) []string {
	return strings.Split(sp.Params["exts"], ",")
}

func TestSpecExtensionsGateTheConnect(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t, WithConnectionTypes(specExtType{stubType{ext: "json", catalogs: true}}))
	// json is installed and loadable; the second one, per spec, is not.
	require.NoError(t, s.Register(Spec{Name: "a", Type: "stub", Params: map[string]string{"exts": "json,definitely_not_an_extension"}, Credential: credOK("t")}))
	require.NoError(t, s.Register(Spec{Name: "b", Type: "stub", Params: map[string]string{"exts": "json"}, Credential: credOK("t")}))
	s.Preflight(ctx, "a")
	st := status(t, s, "a")
	assert.Equal(t, StatusExtensionMissing, st.Kind)
	assert.Equal(t, "definitely_not_an_extension", st.Extension, "the first missing one, from the spec's list")
	require.Error(t, s.Connect(ctx, "a"))
	// A spec that needs only what is there connects.
	require.NoError(t, s.Connect(ctx, "b"))
	assert.Equal(t, StatusOK, status(t, s, "b").Kind)
	// A type without the hook keeps using Extensions().
	assert.Equal(t, []string{"json"}, ExtensionsFor(stubType{ext: "json"}, Spec{}))
	assert.Equal(t, []string{"json", "x"}, ExtensionsFor(specExtType{}, Spec{Params: map[string]string{"exts": "json,x"}}))
}
