package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

const tcNeedle = "pw_needle_testcred"

func resolved(t *testing.T, ws Workspace, d ConnectionDef) ResolvedConnection {
	t.Helper()
	rc, err := ResolveConnection(ws, Connection{ConnectionDef: d, Scope: ScopeShared}, nil)
	require.NoError(t, err)
	return rc
}

func savedRC(t *testing.T, ws Workspace, name string) ResolvedConnection {
	t.Helper()
	rc, err := ResolveConnection(ws, mustConn(t, ws, name), nil)
	require.NoError(t, err)
	return rc
}

func TestTestCredential_UsesTheSavedSignInWhenNothingChanged(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "rosa", Password: tcNeedle}, false))

	cred, err := TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	require.NoError(t, err)
	assert.Equal(t, tcNeedle, cred.Password)

	// What is typed in this edit wins over what is stored.
	cred, err = TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{Password: "typed"})
	require.NoError(t, err)
	assert.Equal(t, "typed", cred.Password)
	assert.Equal(t, "rosa", cred.User)
}

func TestTestCredential_EditedServerNeverReusesAStoredSecret(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "rosa", Password: tcNeedle}, false))

	edited := pgConn("pg")
	edited.Host = "other.internal"
	rc := resolved(t, ws, edited)

	cred, err := TestCredential(ctx, ws, rc, "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fp)
	assert.Contains(t, fp.New, "other.internal")
	assert.Empty(t, cred.Password)
	assert.False(t, cred.HasSecret())

	// A prefilled user name is not a typed secret.
	_, err = TestCredential(ctx, ws, rc, "", secrets.Credential{User: "rosa"})
	require.ErrorAs(t, err, &fp)

	// A typed secret is used, and nothing stored is mixed in.
	cred, err = TestCredential(ctx, ws, rc, "", secrets.Credential{User: "bob", Password: "typed"})
	require.NoError(t, err)
	assert.Equal(t, "typed", cred.Password)
	assert.Equal(t, "bob", cred.User)
	assert.NotContains(t, cred.Password, tcNeedle)
}

func TestTestCredential_ASavedSignInThatNoLongerMatchesIsWithheld(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "rosa", Password: tcNeedle}, false))
	// A pull re-points the saved definition; the test is of that same definition.
	up := pgConn("pg")
	up.Host = "pulled.internal"
	require.NoError(t, ws.UpdateConnection(ScopeShared, up))

	cred, err := TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fp)
	assert.False(t, cred.HasSecret())
}

func TestTestCredential_EnvSignInIsBoundToItsDefinition(t *testing.T) {
	ws, _, _ := memWorkspace(t, map[string]string{"PGP_USER": "svc", "PGP_PASSWORD": tcNeedle})
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetSignIn("pg", "env:PGP"))

	cred, err := TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	require.NoError(t, err)
	assert.Equal(t, tcNeedle, cred.Password)

	// An edit to the host withholds the environment password too.
	edited := pgConn("pg")
	edited.Host = "attacker.example"
	cred, err = TestCredential(ctx, ws, resolved(t, ws, edited), "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fp)
	assert.False(t, cred.HasSecret())

	// So does a pull.
	require.NoError(t, ws.UpdateConnection(ScopeShared, edited))
	cred, err = TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	require.ErrorAs(t, err, &fp)
	assert.False(t, cred.HasSecret())
}

func TestTestCredential_ANewConnectionHasNothingStoredButMayUseEnv(t *testing.T) {
	ws, _, _ := memWorkspace(t, map[string]string{"NEW_USER": "svc", "NEW_PASSWORD": tcNeedle})
	ctx := context.Background()
	rc := resolved(t, ws, pgConn("fresh"))

	_, err := TestCredential(ctx, ws, rc, "", secrets.Credential{})
	require.NoError(t, err, "a keychain sign-in for a connection that does not exist has nothing to find")
	cred, err := TestCredential(ctx, ws, rc, "env:NEW", secrets.Credential{})
	require.NoError(t, err)
	assert.Equal(t, tcNeedle, cred.Password)
}

func TestTestCredential_ReportsAMissingCredentialUnlessTypedMakesUpForIt(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))

	_, err := TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	var nc *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	cred, err := TestCredential(ctx, ws, savedRC(t, ws, "pg"), "", secrets.Credential{Password: "typed"})
	require.NoError(t, err)
	assert.Equal(t, "typed", cred.Password)
}

// fixedSession reports fixed statuses.
type fixedSession struct {
	session.Session
	st []session.Status
}

func (f fixedSession) Connections() []session.Status           { return f.st }
func (fixedSession) ConnectionInfos() []session.ConnectionInfo { return nil }

func TestConnectionStates_TypedStatusAndServerChange(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "ghost", Type: "sqlite", Path: "${NOPE_UNSET}/x.sqlite"}))
	sess := fixedSession{st: []session.Status{{
		Name: "pg", Type: "postgres", Kind: session.StatusNeedsCredentials,
		Message:       "the server changed since you signed in (old.example to new.example). Sign in again",
		ServerChanged: &session.ServerChange{Old: "old.example", New: "new.example"},
	}}}
	issues := []SessionIssue{{Connection: "ghost", Message: "connection \"ghost\": path: environment variable NOPE_UNSET is not set"}}

	by := map[string]ConnectionState{}
	for _, st := range ConnectionStates(ws, sess, issues) {
		by[st.Name] = st
	}
	pg := by["pg"]
	assert.Equal(t, session.StatusNeedsCredentials, pg.Status)
	require.NotNil(t, pg.ServerChanged)
	assert.Equal(t, "old.example", pg.ServerChanged.Old)
	assert.Equal(t, "new.example", pg.ServerChanged.New)
	assert.True(t, pg.Remote)

	// A definition that could not even be registered is an error, the word every surface uses.
	ghost := by["ghost"]
	assert.Equal(t, session.StatusError, ghost.Status)
	assert.NotEmpty(t, ghost.Detail)
}

// lockedSession is a session whose only connection is held by another process.
type lockedSession struct{ session.Session }

func (lockedSession) Connections() []session.Status {
	return []session.Status{{Name: "held", Type: "duckdb", Kind: session.StatusLockedBy, Message: "Conflicting lock is held (PID 4242)", LockedByPID: 4242}}
}
func (lockedSession) ConnectionInfos() []session.ConnectionInfo { return nil }

func TestConnectionStates_LockedIsReadFromTheTypedStatus(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "held", Type: "duckdb", Path: "data/held.duckdb"}))
	// The issue's wording is irrelevant: only the typed status says "locked".
	issues := []SessionIssue{{Connection: "held", Message: "some reworded driver text"}}
	var st ConnectionState
	for _, s := range ConnectionStates(ws, lockedSession{}, issues) {
		if s.Name == "held" {
			st = s
		}
	}
	assert.Equal(t, session.StatusLockedBy, st.Status)
	assert.True(t, st.Locked())
	assert.Equal(t, 4242, st.LockedByPID)
}

// panicResolverWS fails the test if the keychain/resolver is touched.
func TestTestCredential_NoCredentialTypesNeverTouchTheKeychain(t *testing.T) {
	ws, store, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "lite", Type: "sqlite", Path: "x.sqlite"}))
	require.NoError(t, store.Set(context.Background(), secrets.WorkspaceRef(ws.ID(), "lite"), []byte("not json")))
	cred, err := TestCredential(context.Background(), ws, savedRC(t, ws, "lite"), "", secrets.Credential{})
	require.NoError(t, err)
	assert.False(t, cred.HasSecret())
}

func TestTestCredential_ServerChangedWithoutAKnownOldServerIsGeneric(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "r", Password: tcNeedle}, false))
	edited := pgConn("pg")
	edited.Host = "other.internal"
	_, err := TestCredential(ctx, ws, resolved(t, ws, edited), "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fp)
	assert.NotEqual(t, fp.Old, fp.New, "never \"X to X\"")
}
