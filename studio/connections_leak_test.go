package main

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const leakNeedle = "pw-R117-NEEDLE"

// fakePG asks for a cleartext password and records what it gets.
type fakePG struct {
	ln  net.Listener
	mu  sync.Mutex
	got []string
}

func startFakePG(t *testing.T) *fakePG {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakePG{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakePG) port() int {
	a, _ := f.ln.Addr().(*net.TCPAddr)
	return a.Port
}

func (f *fakePG) serve(c net.Conn) {
	defer c.Close() //nolint:errcheck
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(c, hdr[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint32(hdr[:]))
		body := make([]byte, n-4)
		if _, err := io.ReadFull(c, body); err != nil {
			return
		}
		code := binary.BigEndian.Uint32(body[:4])
		if code == 80877103 || code == 80877104 { // SSL / GSSENC request
			_, _ = c.Write([]byte("N"))
			continue
		}
		f.mu.Lock()
		f.got = append(f.got, "startup:"+strings.ReplaceAll(string(body[4:]), "\x00", "|"))
		f.mu.Unlock()
		break
	}
	// AuthenticationCleartextPassword
	_, _ = c.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 3})
	var t [1]byte
	if _, err := io.ReadFull(c, t[:]); err != nil {
		return
	}
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	body := make([]byte, n-4)
	_, _ = io.ReadFull(c, body)
	f.mu.Lock()
	f.got = append(f.got, "password:"+strings.TrimRight(string(body), "\x00"))
	f.mu.Unlock()
	msg := "SFATAL\x00C28P01\x00Mpassword authentication failed for user\x00\x00"
	out := []byte{'E', 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:], uint32(4+len(msg)))
	_, _ = c.Write(append(out, msg...))
}

func (f *fakePG) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.got, "\n")
}

func leakApp(t *testing.T, env map[string]string) *App {
	app := newBareApp(t)
	m := &secrets.Mock{}
	app.secrets = secrets.NewResolver(secrets.WithKeychain(m), secrets.WithRedactor(app.secrets.Redactor()), secrets.WithEnv(func(k string) string { return env[k] }))
	res, err := app.OpenWorkspace(newWorkspaceDir(t, "R117"))
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	return app
}

// A stored credential is never sent to a server other than the one it was saved
// for, by any sign-in method, from the editor's Test (review #117). Only what is
// typed in the edit is used; without it Test says why and sends nothing.
const serverChangedMsg = "The server changed, so your saved sign-in won't be used. Enter it again to test."

func TestLeak_TestKeychainCredNotSentToNewHost(t *testing.T) {
	fake := startFakePG(t)
	app := leakApp(t, nil)
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "app"}, IsNew: true, SignIn: "keychain"})
	require.NoError(t, err)
	_, err = app.SetConnectionCredentials(SetCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa", Password: leakNeedle}})
	require.NoError(t, err)
	newDef := workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "127.0.0.1", Port: fake.port(), Database: "app"}
	res, err := app.TestConnection(TestConnectionRequest{Scope: "shared", Def: newDef})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Equal(t, serverChangedMsg, res.Error.Message)
	assert.Equal(t, "server-changed", res.Error.Code)
	assert.Empty(t, fake.all(), "nothing was sent to the new host")

	// The readable username the editor prefills is not a typed credential.
	res, err = app.TestConnection(TestConnectionRequest{Scope: "shared", Def: newDef, Credentials: CredentialInput{Username: "rosa"}})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Equal(t, serverChangedMsg, res.Error.Message)
	assert.Empty(t, fake.all())

	// What is typed in this edit is the person's own and is used.
	res, err = app.TestConnection(TestConnectionRequest{Scope: "shared", Def: newDef, Credentials: CredentialInput{Username: "rosa", Password: "typed-now"}})
	require.NoError(t, err)
	assert.False(t, res.OK)
	if res.InstallExtension == "" { // the postgres extension is installed on this machine
		assert.Contains(t, fake.all(), "typed-now")
	}
	assert.NotContains(t, fake.all(), leakNeedle)
}

func TestLeak_TestEnvCredAfterServerChange(t *testing.T) {
	fake := startFakePG(t)
	app := leakApp(t, map[string]string{"PGPROD_USER": "rosa", "PGPROD_PASSWORD": leakNeedle})
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "app"}, IsNew: true, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	// A pull points prod_pg at another server.
	yml := filepath.Join(app.ws.Root(), "vibe.yaml")
	b, err := os.ReadFile(yml)
	require.NoError(t, err)
	nb := strings.ReplaceAll(string(b), "db.internal", "127.0.0.1")
	nb = strings.ReplaceAll(nb, "port: 5432", "port: "+strconv.Itoa(fake.port()))
	require.Contains(t, nb, "port: "+strconv.Itoa(fake.port()))
	require.NoError(t, os.WriteFile(yml, []byte(nb), 0o600))
	require.NoError(t, app.ws.Reload())
	app.syncConnections()

	// The session refuses to use it...
	_, _ = app.ConnectConnection(ConnectionRef{Name: "prod_pg"})
	assert.NotContains(t, fake.all(), leakNeedle, "session connect")

	// ...and so does the editor's Test, opened on the pulled definition with nothing typed.
	d, err := app.GetConnection("prod_pg")
	require.NoError(t, err)
	res, err := app.TestConnection(TestConnectionRequest{Scope: d.Scope, Def: d.Def, SignIn: d.SignIn})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Equal(t, serverChangedMsg, res.Error.Message)
	assert.NotContains(t, fake.all(), leakNeedle, "LEAK: env credential sent to a server-changed host by Test")
}

func TestLeak_TestEnvCredStillWorksWhenNothingChanged(t *testing.T) {
	fake := startFakePG(t)
	app := leakApp(t, map[string]string{"PGPROD_USER": "rosa", "PGPROD_PASSWORD": "env-pw"})
	def := workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "127.0.0.1", Port: fake.port(), Database: "app"}
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: def, IsNew: true, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	res, err := app.TestConnection(TestConnectionRequest{Scope: "shared", Def: def, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	assert.False(t, res.OK) // the fake always refuses; what matters is that the saved env sign-in was used
	if res.InstallExtension == "" {
		assert.Contains(t, fake.all(), "env-pw")
	}
}

// ---- the expanded matrix (re-review #117): every route a stored credential could
// take to a server it was not chosen for, for both keychain and env sign-ins ----

func leakPulledApp(t *testing.T, method string) (*App, *fakePG) {
	t.Helper()
	fake := startFakePG(t)
	app := leakApp(t, map[string]string{"PGPROD_USER": "rosa", "PGPROD_PASSWORD": leakNeedle})
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "app"}, IsNew: true, SignIn: method})
	require.NoError(t, err)
	if method == "keychain" {
		_, err = app.SetConnectionCredentials(SetCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa", Password: leakNeedle}})
		require.NoError(t, err)
	}
	yml := filepath.Join(app.ws.Root(), "vibe.yaml")
	b, err := os.ReadFile(yml)
	require.NoError(t, err)
	nb := strings.ReplaceAll(string(b), "db.internal", "127.0.0.1")
	nb = strings.ReplaceAll(nb, "port: 5432", "port: "+strconv.Itoa(fake.port()))
	require.Contains(t, nb, "port: "+strconv.Itoa(fake.port()))
	require.NoError(t, os.WriteFile(yml, []byte(nb), 0o600))
	require.NoError(t, app.ws.Reload())
	app.syncConnections()
	return app, fake
}

func TestLeak_EveryRouteAfterAPull(t *testing.T) {
	for _, method := range []string{"keychain", "env:PGPROD"} {
		t.Run(method, func(t *testing.T) {
			app, fake := leakPulledApp(t, method)
			noLeak := func(step string) {
				t.Helper()
				assert.NotContains(t, fake.all(), leakNeedle, "LEAK via %s (%s)", step, method)
			}
			_, _ = app.ConnectConnection(ConnectionRef{Name: "prod_pg"})
			noLeak("ConnectConnection")
			_, _ = app.RunQuery(RunQueryRequest{SQL: "SELECT * FROM prod_pg.public.x"})
			noLeak("RunQuery lazy attach")
			_, _ = app.RunQuery(RunQueryRequest{SQL: "SELECT 1", Connection: "prod_pg"})
			noLeak("RunQuery pinned")

			d, err := app.GetConnection("prod_pg")
			require.NoError(t, err)
			// The rail says the server changed, for env as well as the keychain.
			require.NotNil(t, d.Info.ServerChanged, "status: %+v", d.Info)
			assert.Equal(t, "db.internal:5432/app", d.Info.ServerChanged.Old)
			assert.False(t, d.Credential.FingerprintMatches)

			res, _ := app.TestConnection(TestConnectionRequest{Scope: d.Scope, Def: d.Def, SignIn: d.SignIn})
			require.NotNil(t, res.Error)
			assert.Equal(t, serverChangedMsg, res.Error.Message)
			noLeak("TestConnection")

			// A duplicate starts at the keychain with nothing saved: it has no stored credential.
			dup := d.Def
			dup.Name = "prod_pg_copy"
			res, _ = app.TestConnection(TestConnectionRequest{Scope: "local", Def: dup, SignIn: "keychain"})
			require.NotNil(t, res.Error)
			noLeak("TestConnection duplicate")

			// The sign-in prompt with no secret typed uses nothing stored.
			_, _ = app.ConnectWithCredentials(ConnectWithCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa"}, Remember: true})
			noLeak("ConnectWithCredentials remember")
			_, _ = app.ConnectWithCredentials(ConnectWithCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa"}, Remember: false})
			noLeak("ConnectWithCredentials session")

			// A plain Save (no confirmation) cannot re-bind: the binding is unchanged and
			// the next connect still sends nothing.
			_, err = app.SaveConnection(SaveConnectionRequest{Scope: d.Scope, Def: d.Def, SignIn: d.SignIn})
			if method != "keychain" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), rebindPrefix)
			}
			_, _ = app.ConnectConnection(ConnectionRef{Name: "prod_pg"})
			noLeak("ConnectConnection after a plain Save")
			// Nor can the other bindings that call SetSignIn.
			if method != "keychain" {
				_, err = app.SetConnectionCredentials(SetCredentialsRequest{Name: "prod_pg", Method: "env:PGPROD"})
				_, _ = app.ConnectConnection(ConnectionRef{Name: "prod_pg"})
				noLeak("SetConnectionCredentials re-bind")
				_ = err
			}
		})
	}
}

func TestSetSignIn_RefusesASilentRebind_AndConfirmedRebindWorks(t *testing.T) {
	fake := startFakePG(t)
	app := leakApp(t, map[string]string{"PGPROD_USER": "rosa", "PGPROD_PASSWORD": "env-pw"})
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "app"}, IsNew: true, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	// The editor changes the host and saves: the environment sign-in would follow it.
	moved := workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "127.0.0.1", Port: fake.port(), Database: "app"}
	_, err = app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: moved, SignIn: "env:PGPROD"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), rebindPrefix)
	d, err := app.GetConnection("prod_pg")
	require.NoError(t, err)
	assert.False(t, d.Credential.FingerprintMatches, "the binding is unchanged: still the old server")
	assert.Equal(t, "db.internal", d.Info.ServerChanged.Old[:11])
	_, _ = app.ConnectConnection(ConnectionRef{Name: "prod_pg"})
	assert.NotContains(t, fake.all(), "env-pw")

	// With the confirmation the person gave in the UI, it re-binds.
	_, err = app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: moved, SignIn: "env:PGPROD", ConfirmRebind: true})
	require.NoError(t, err)
	d, err = app.GetConnection("prod_pg")
	require.NoError(t, err)
	assert.True(t, d.Credential.FingerprintMatches)

	// A first binding, and a switch to the keychain, never need confirming.
	_, err = app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: moved, SignIn: "keychain"})
	require.NoError(t, err)
}

func TestSessionCredentials_DoNotSurviveCloseAndReopen(t *testing.T) {
	app, m := connApp(t)
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "local", Def: pgDef("127.0.0.1"), IsNew: true, SignIn: "keychain"})
	require.NoError(t, err)
	_, _ = app.ConnectWithCredentials(ConnectWithCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa", Password: connNeedle}, Remember: false})
	d, _ := app.GetConnection("prod_pg")
	require.True(t, d.Credential.SecretSet)

	// Clearing one field of a session-only credential works (no "not found").
	_, err = app.ClearConnectionCredentials(ClearCredentialsRequest{Name: "prod_pg", Field: "password"})
	require.NoError(t, err)
	d, _ = app.GetConnection("prod_pg")
	assert.False(t, d.Credential.SecretSet)

	// Ask-each-time credentials too: held in memory, gone on close.
	_, _ = app.ConnectWithCredentials(ConnectWithCredentialsRequest{Name: "prod_pg", CredentialInput: CredentialInput{Username: "rosa", Password: connNeedle}, Remember: false})
	root := app.ws.Root()
	require.NoError(t, app.CloseWorkspace())
	_, err = app.OpenWorkspace(root)
	require.NoError(t, err)
	d, err = app.GetConnection("prod_pg")
	require.NoError(t, err)
	assert.False(t, d.Credential.SecretSet, "the session-only credential did not survive the close")
	assert.Empty(t, d.Credential.Username)
	for _, c := range m.Calls {
		assert.False(t, strings.HasPrefix(c, "set "), "never written to the keychain")
	}

	// Nothing saved at all: clearing a field is a plain sentence, not a store error.
	_, err = app.ClearConnectionCredentials(ClearCredentialsRequest{Name: "prod_pg", Field: "password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing saved to clear")
}

// An environment sign-in never uses a token typed into the (hidden) token field.
func TestListMotherDuckDatabases_EnvSignInIgnoresATypedToken(t *testing.T) {
	app := leakApp(t, nil) // MOTHERDUCK_TOKEN is not set
	res, err := app.ListMotherDuckDatabases(ListMotherDuckDatabasesRequest{
		SignIn: "env:MOTHERDUCK", Credentials: CredentialInput{Token: leakNeedle},
	})
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Equal(t, string(session.StatusNeedsCredentials), res.Status)
	assert.Empty(t, res.Source, "no typed token was used")
	assert.NotContains(t, res.Message, leakNeedle)
}
