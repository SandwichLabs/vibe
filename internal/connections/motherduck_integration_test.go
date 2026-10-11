//go:build integration

package connections_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

// The hub scrubs MOTHERDUCK_TOKEN from the process environment when it opens,
// so the real token is captured before any test runs.
var realMDToken = os.Getenv("MOTHERDUCK_TOKEN")

const fakeMDToken = "eyFAKE.md_token_needle_9981.sig"

func installMD(t *testing.T) string {
	t.Helper()
	dir := extDir(t)
	db, err := session.OpenDB("", session.WithExtensionDirectory(dir))
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	_, err = db.Exec("INSTALL motherduck")
	require.NoError(t, err, "INSTALL motherduck (needs network)")
	return dir
}

func credFn(c secrets.Credential) session.CredentialFunc {
	return func(context.Context) (secrets.Credential, error) { return c, nil }
}

// No real token needed: the hub-open ordering, env scrubbing, the relaunch
// requirement for a second account, and a bad token not hanging or leaking.
func TestMotherDuckHubOpenWithAFakeToken(t *testing.T) {
	dir := installMD(t)
	t.Setenv("MOTHERDUCK_TOKEN", "ambient_needle_env")
	sess, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(dir),
		session.WithMotherDuckToken(fakeMDToken))
	require.NoError(t, err)
	defer sess.Close()
	_, ambient := os.LookupEnv("MOTHERDUCK_TOKEN")
	assert.False(t, ambient, "the ambient token is scrubbed before LOAD")

	var tok sql.NullString
	require.NoError(t, sess.DB().QueryRow("SELECT CAST(current_setting('motherduck_token') AS VARCHAR)").Scan(&tok))
	assert.NotContains(t, tok.String, "ambient_needle_env")
	assert.NotContains(t, tok.String, fakeMDToken, "reads back redacted")

	ctx := context.Background()
	// A different account in the same workspace needs a relaunch, and never dials.
	require.NoError(t, sess.Register(session.Spec{Name: "other", Type: "motherduck", Params: map[string]string{"database": "other_db"},
		Credential: credFn(secrets.Credential{Token: "a_different_token"})}))
	require.Error(t, sess.Connect(ctx, "other"))
	relaunch, why := sess.RelaunchRequired()
	assert.True(t, relaunch)
	assert.Contains(t, why, "different MotherDuck account")

	// The same (fake) token dials; MotherDuck rejects it, quickly, without
	// echoing it, and without a browser flow.
	require.NoError(t, sess.Register(session.Spec{Name: "md", Type: "motherduck", Params: map[string]string{"database": "nosuchdb"},
		Credential: credFn(secrets.Credential{Token: fakeMDToken})}))
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	err = sess.Connect(cctx, "md")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 25*time.Second, "no SSO hang")
	assert.NotContains(t, err.Error(), fakeMDToken)
	st := connStatus(sess, "md")
	assert.Contains(t, []session.StatusKind{session.StatusNeedsCredentials, session.StatusError}, st.Kind)
	t.Logf("fake token attach: %s / %s", st.Kind, st.Message)
}

// With the extension installed after the workspace opened, the token cannot be
// applied (lock_configuration): the session asks for a relaunch.
func TestMotherDuckInstalledAfterOpenNeedsARelaunch(t *testing.T) {
	_ = installMD(t)
	fresh := t.TempDir()
	sess, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(fresh),
		session.WithMotherDuckToken(fakeMDToken))
	require.NoError(t, err)
	defer sess.Close()
	ctx := context.Background()
	require.NoError(t, sess.Register(session.Spec{Name: "md", Type: "motherduck", Params: map[string]string{"database": "d"},
		Credential: credFn(secrets.Credential{Token: fakeMDToken})}))
	sess.Preflight(ctx, "md")
	assert.Equal(t, session.StatusExtensionMissing, connStatus(sess, "md").Kind)
	require.NoError(t, sess.InstallExtension(ctx, "motherduck"))
	relaunch, why := sess.RelaunchRequired()
	assert.True(t, relaunch)
	assert.Contains(t, why, "Reopen")
	require.Error(t, sess.Connect(ctx, "md"), "never attaches md: without the token SET")
}

// Needs a real token (and a database name): skipped otherwise.
func TestMotherDuckRealToken(t *testing.T) {
	tok, db := realMDToken, os.Getenv("MOTHERDUCK_TEST_DATABASE")
	if tok == "" {
		t.Skip("MOTHERDUCK_TOKEN is not set")
	}
	if db == "" {
		t.Skip("MOTHERDUCK_TEST_DATABASE is not set")
	}
	exe, err := os.Executable()
	require.NoError(t, err)
	res := connections.Test(context.Background(), connections.Def{Name: "md", Type: "motherduck", Database: db},
		secrets.Credential{Token: tok}, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)), connections.WithExecutable(exe))
	t.Logf("%+v", res)
	assert.True(t, res.OK, res.Error)
}

// Needs a real token: skipped otherwise. Attaching one named database adds
// exactly that catalog (plus MotherDuck's own md_information_schema), and the
// database list the editor shows includes it (#163).
func TestMotherDuckAttachesExactlyTheNamedDatabase(t *testing.T) {
	tok := realMDToken
	if tok == "" {
		t.Skip("MOTHERDUCK_TOKEN is not set")
	}
	_ = installMD(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	list := connections.ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: tok},
		connections.WithExecutable(exe), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, list.OK, list.Error)
	require.NotEmpty(t, list.Databases, "the account has at least one database")
	db := os.Getenv("MOTHERDUCK_TEST_DATABASE")
	if db == "" {
		db = list.Databases[0].Name
	}
	var names []string
	for _, d := range list.Databases {
		names = append(names, d.Name)
	}
	assert.Contains(t, names, db)
	assert.NotContains(t, names, "memory", "the local in-memory catalog is not a MotherDuck database")

	sess, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(extDir(t)),
		session.WithMotherDuckToken(tok))
	require.NoError(t, err)
	defer sess.Close()
	ctx := context.Background()
	require.NoError(t, sess.Register(session.Spec{Name: "md", Type: "motherduck", Params: map[string]string{"database": db},
		Credential: credFn(secrets.Credential{Token: tok})}))
	require.NoError(t, sess.Connect(ctx, "md"))
	rows, err := sess.DB().QueryContext(ctx, `SELECT database_name FROM duckdb_databases() WHERE type = 'motherduck'`)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck
	var got []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		got = append(got, n)
	}
	assert.Equal(t, []string{"md"}, got, "exactly one MotherDuck catalog, not every database of the account")
}
