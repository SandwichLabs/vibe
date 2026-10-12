package connections

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

func emptyExt(t *testing.T) TestOption { return WithTestExtensionDir(t.TempDir()) }

func TestTestRejectsAnInvalidDefinitionWithoutDialling(t *testing.T) {
	res := Test(context.Background(), Def{Name: "pg", Type: "postgres"}, secrets.Credential{}, emptyExt(t))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "needs a host")
}

func TestTestReportsAMissingExtensionAndDoesNotInstall(t *testing.T) {
	ext := t.TempDir()
	cred := secrets.Credential{User: "u", Password: "pw_needle_9"}
	res := Test(context.Background(), pgDef(), cred, WithTestExtensionDir(ext))
	assert.False(t, res.OK)
	assert.Equal(t, session.StatusExtensionMissing, res.Status)
	assert.Equal(t, "postgres", res.InstallExtension)
	assert.NotContains(t, res.Error, "pw_needle_9")
	entries, _ := os.ReadDir(ext)
	assert.Empty(t, entries, "without consent nothing is downloaded into the extension directory")
}

func TestTestNoCredentialIsNeedsCredentials(t *testing.T) {
	// Catalogless storage with an empty credential: the secret step refuses.
	res := Test(context.Background(), Def{Name: "lake", Type: "s3", Scope: "s3://b/"}, secrets.Credential{}, emptyExt(t))
	assert.False(t, res.OK)
	assert.NotEqual(t, session.StatusOK, res.Status)
	res = Test(context.Background(), Def{Name: "md", Type: "motherduck", Database: "d"}, secrets.Credential{}, emptyExt(t))
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
	assert.Contains(t, res.Error, "token")
}

func TestTestTimesOut(t *testing.T) {
	res := Test(context.Background(), pgDef(), secrets.Credential{User: "u"}, emptyExt(t), WithTestTimeout(time.Nanosecond))
	assert.False(t, res.OK)
}

func TestTestDuckDBFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.duckdb")
	mk := Test(context.Background(), Def{Name: "f", Type: "duckdb", Path: p}, secrets.Credential{}, emptyExt(t))
	assert.False(t, mk.OK, "a missing file is reported, not created (a test attaches read-only)")
}

// --- MotherDuck subprocess ---

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-vibe")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755)) //nolint:gosec // test script
	return p
}

func TestMotherDuckTestRunsInASubprocessAndTheTokenOnlyTravelsOnStdin(t *testing.T) {
	t.Setenv("MOTHERDUCK_TOKEN", "ambient_env_needle")
	t.Setenv("motherduck_token", "ambient_env_needle_lc")
	out := filepath.Join(t.TempDir(), "seen")
	exe := writeScript(t, `{ echo "ARGS: $@"; env; echo "STDIN:"; cat; } > `+out+`
echo 'noise from the extension'
echo '{"ok":true,"status":"ok","count":3,"detail":"3 tables"}'
`)
	def := Def{Name: "md", Type: "motherduck", Database: "my_db"}
	res := Test(context.Background(), def, secrets.Credential{Token: "md_token_needle_77"}, WithExecutable(exe), emptyExt(t))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, 3, res.Count)

	seen, err := os.ReadFile(out)
	require.NoError(t, err)
	s := string(seen)
	args := s[:strings.Index(s, "\n")]
	assert.Equal(t, "ARGS: "+ChildCommand, args)
	env := s[:strings.Index(s, "STDIN:")]
	assert.NotContains(t, env, "md_token_needle_77", "the token is not in argv or the environment")
	assert.NotContains(t, strings.ToLower(env), "motherduck_token", "ambient MotherDuck variables are stripped from the child")
	stdin := s[strings.Index(s, "STDIN:"):]
	assert.Contains(t, stdin, "md_token_needle_77", "the token travels on stdin")
}

func TestMotherDuckSubprocessResultIsRedactedAndFailuresAreReported(t *testing.T) {
	exe := writeScript(t, `cat >/dev/null
echo '{"ok":false,"status":"error","error":"Request failed: token md_token_needle_88 bad"}'
`)
	def := Def{Name: "md", Type: "motherduck", Database: "my_db"}
	res := Test(context.Background(), def, secrets.Credential{Token: "md_token_needle_88"}, WithExecutable(exe), emptyExt(t))
	assert.False(t, res.OK)
	assert.NotContains(t, res.Error, "md_token_needle_88")

	exe = writeScript(t, "cat >/dev/null\nexit 3\n")
	res = Test(context.Background(), def, secrets.Credential{Token: "t"}, WithExecutable(exe), emptyExt(t))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "test process failed")

	exe = writeScript(t, "cat >/dev/null\nsleep 5\n")
	start := time.Now()
	res = Test(context.Background(), def, secrets.Credential{Token: "t"}, WithExecutable(exe), emptyExt(t), WithTestTimeout(300*time.Millisecond))
	assert.False(t, res.OK)
	assert.Less(t, time.Since(start), 3*time.Second, "the subprocess is killed at the timeout")
}

func TestMotherDuckSubprocessWithoutAnExtension(t *testing.T) {
	// The real child (this test binary): no extension installed, no consent.
	exe, err := os.Executable()
	require.NoError(t, err)
	def := Def{Name: "md", Type: "motherduck", Database: "my_db"}
	res := Test(context.Background(), def, secrets.Credential{Token: "md_token_needle_99"}, WithExecutable(exe), emptyExt(t))
	assert.False(t, res.OK)
	assert.Equal(t, session.StatusExtensionMissing, res.Status)
	assert.Equal(t, "motherduck", res.InstallExtension)
	assert.NotContains(t, res.Error, "md_token_needle_99")
}

func TestMotherDuckTestWithoutATokenNeverStartsAProcess(t *testing.T) {
	exe := writeScript(t, "touch "+filepath.Join(t.TempDir(), "never")+"\nexit 9\n")
	res := Test(context.Background(), Def{Name: "md", Type: "motherduck", Database: "d"}, secrets.Credential{}, WithExecutable(exe), emptyExt(t))
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
}

func TestChildEnvDropsAmbientTokens(t *testing.T) {
	got := childEnv([]string{"A=1", "MOTHERDUCK_TOKEN=x", "motherduck_token=y", "PATH=/bin", "MotherDuck_Token=z", "MOTHERDUCK_HOME=/h"})
	assert.Equal(t, []string{"A=1", "PATH=/bin", "MOTHERDUCK_HOME=/h"}, got)
}

func TestClassify(t *testing.T) {
	def := Def{Type: "postgres", Host: "db.staging.internal", Database: "app"}
	cases := []struct{ raw, code string }{
		{"password authentication failed for user", CodeBadSignin},
		{"timed out after 10s", CodeTimeout},
		{`FATAL: database "app" does not exist`, CodeDBNotFound},
		{"could not connect to server: Connection refused", CodeUnreachable},
		{"server requires SSL", CodeSSLRequired},
		{"weird", CodeError},
	}
	for _, c := range cases {
		code, msg := Classify(Failure{Raw: c.raw, Def: def})
		assert.Equal(t, c.code, code, c.raw)
		assert.NotEmpty(t, msg)
	}
	code, msg := Classify(Failure{Status: session.StatusExtensionMissing, InstallExtension: "postgres", Raw: "x", Def: def})
	assert.Equal(t, CodeExtensionMissing, code)
	assert.Contains(t, msg, "postgres")
	_, msg = Classify(Failure{Raw: "password authentication failed", Def: def})
	assert.Contains(t, msg, "db.staging.internal")
	// A deadline is a timeout whatever the message says.
	code, _ = Classify(Failure{Raw: "weird", TimedOut: true, Def: def})
	assert.Equal(t, CodeTimeout, code)
	// A rejected key reads needs-credentials and gets the service wording.
	code, msg = Classify(Failure{Raw: "403", Status: session.StatusNeedsCredentials, Def: Def{Type: "s3"}})
	assert.Equal(t, CodeBadSignin, code)
	assert.Contains(t, msg, "keys or this token")
}

func TestTest_FillsCodeAndMessageWhereTheErrorIsProduced(t *testing.T) {
	res := Test(context.Background(), Def{Name: "x", Type: "postgres"}, secrets.Credential{})
	require.False(t, res.OK)
	assert.NotEmpty(t, res.Code)
	assert.NotEmpty(t, res.Message)
}
