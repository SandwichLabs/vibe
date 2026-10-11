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

// Regression (#163): the extension reads everything after "md:" as the
// database name, so a "?attach_mode=single" suffix made every attach fail.
func TestMotherDuckAttachSQLHasNoQueryParameters(t *testing.T) {
	stmts, err := motherduckType{}.AttachSQL(session.Spec{Name: "md", Type: "motherduck", Params: map[string]string{"database": "my_db"}})
	require.NoError(t, err)
	require.Equal(t, []string{`ATTACH 'md:my_db' AS "md"`}, stmts)
	assert.NotContains(t, stmts[0], "?")
	assert.NotContains(t, stmts[0], "attach_mode")
	// Bare "md:" would attach every database of the account.
	_, err = motherduckType{}.AttachSQL(session.Spec{Name: "md", Type: "motherduck", Params: map[string]string{"database": ""}})
	assert.Error(t, err)
}

func TestListMotherDuckDatabasesRunsInASubprocessWithTheTokenOnStdin(t *testing.T) {
	t.Setenv("MOTHERDUCK_TOKEN", "ambient_env_needle")
	out := filepath.Join(t.TempDir(), "seen")
	exe := writeScript(t, `{ echo "ARGS: $@"; env; echo "STDIN:"; cat; } > `+out+`
echo 'noise'
echo '{"ok":true,"status":"ok","databases":[{"name":"my_db"},{"name":"sample_data","share":true}]}'
`)
	res := ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: "md_token_needle_41"}, WithExecutable(exe))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, []MDDatabase{{Name: "my_db"}, {Name: "sample_data", Share: true}}, res.Databases)

	seen, err := os.ReadFile(out)
	require.NoError(t, err)
	s := string(seen)
	assert.Equal(t, "ARGS: "+ChildCommand, s[:strings.Index(s, "\n")])
	env := s[:strings.Index(s, "STDIN:")]
	assert.NotContains(t, env, "md_token_needle_41")
	assert.NotContains(t, strings.ToLower(env), "motherduck_token")
	stdin := s[strings.Index(s, "STDIN:"):]
	assert.Contains(t, stdin, "md_token_needle_41")
	assert.Contains(t, stdin, `"action":"list-databases"`)
}

func TestListMotherDuckDatabasesRedactsAndReportsFailures(t *testing.T) {
	exe := writeScript(t, `cat >/dev/null
echo '{"ok":false,"status":"error","error":"bad token md_token_needle_42"}'
`)
	res := ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: "md_token_needle_42"}, WithExecutable(exe))
	assert.False(t, res.OK)
	assert.NotContains(t, res.Error, "md_token_needle_42")

	exe = writeScript(t, "cat >/dev/null\nexit 3\n")
	res = ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: "t"}, WithExecutable(exe))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "process failed")

	exe = writeScript(t, "cat >/dev/null\nsleep 5\n")
	start := time.Now()
	res = ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: "t"}, WithExecutable(exe), WithTestTimeout(300*time.Millisecond))
	assert.False(t, res.OK)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestListMotherDuckDatabasesWithoutATokenNeverStartsAProcess(t *testing.T) {
	exe := writeScript(t, "touch "+filepath.Join(t.TempDir(), "never")+"\nexit 9\n")
	res := ListMotherDuckDatabases(context.Background(), secrets.Credential{}, WithExecutable(exe))
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
}

func TestListMotherDuckDatabasesWithoutAnExtension(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	res := ListMotherDuckDatabases(context.Background(), secrets.Credential{Token: "md_token_needle_43"}, WithExecutable(exe), emptyExt(t))
	assert.False(t, res.OK)
	assert.Equal(t, session.StatusExtensionMissing, res.Status)
	assert.Equal(t, "motherduck", res.InstallExtension)
	assert.NotContains(t, res.Error, "md_token_needle_43")
}
