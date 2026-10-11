package workspace

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testID = "7f3c2a10-5b1e-4c52-9d0e-2a6f1b8c4d90"

func TestParseConfig_Valid(t *testing.T) {
	c, err := ParseConfig([]byte(`
version: 1
id: ` + testID + `
name: Acme Analytics
default_connection: local
sandbox: read-only
ask_before_writes: true
connections:
  - name: prod_pg
    type: postgres
    host: db.internal
    port: 5432
    database: analytics
    credentials: keychain
    options: { sslmode: require }
  - name: local
    type: duckdb
    path: data/local.duckdb
    read_only: false
`))
	require.NoError(t, err)
	assert.Equal(t, "Acme Analytics", c.Name)
	require.Len(t, c.Connections, 2)
	assert.True(t, c.Connections[0].IsReadOnly(), "read_only defaults to true")
	assert.False(t, c.Connections[1].IsReadOnly())
	assert.Equal(t, "require", c.Connections[0].Options["sslmode"])
}

func TestParseConfig_Rejects(t *testing.T) {
	cases := map[string]string{
		"unknown top-level key":  "id: " + testID + "\nbogus: 1\n",
		"unknown connection key": "id: " + testID + "\nconnections:\n  - name: a\n    type: duckdb\n    pasword: x\n",
		"password field":         "id: " + testID + "\nconnections:\n  - name: a\n    type: postgres\n    password: hunter2\n",
		"duplicate key":          "id: " + testID + "\nid: " + testID + "\n",
		"two documents":          "id: " + testID + "\n---\nid: " + testID + "\n",
		"missing id":             "name: x\n",
		"id not uuid":            "id: abc\n",
		"newer version":          "version: 99\nid: " + testID + "\n",
		"bad sandbox":            "id: " + testID + "\nsandbox: root\n",
		"bad name":               "id: " + testID + "\nconnections:\n  - name: Bad-Name\n    type: duckdb\n    path: x\n",
		"reserved name":          "id: " + testID + "\nconnections:\n  - name: main\n    type: duckdb\n    path: x\n",
		"duplicate connection":   "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: x}\n  - {name: a, type: duckdb, path: y}\n",
		"bad credentials":        "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: x, credentials: plaintext}\n",
		"secret option":          "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: h, options: {password: x}}\n",
		"secret in url":          "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: 'postgres://u:pw@db/x'}\n",
		"secret in path pair":    "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: 'u:pw@db'}\n",
		"bad port":               "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: h, port: 70000}\n",
		"bad default":            "id: " + testID + "\ndefault_connection: Nope!\n",
		"env in shared host":     "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: '${PG_HOST}'}\n",
		"env in shared path":     "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: '${HOME}/x.duckdb'}\n",
		"env in shared option":   "id: " + testID + "\nconnections:\n  - {name: a, type: postgres, host: h, options: {application_name: 'x-${USER}'}}\n",
		"absolute shared path":   "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: /tmp/evil.duckdb}\n",
		"windows abs path":       "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: 'C:\\\\x\\\\y.duckdb'}\n",
		"home shared path":       "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: '~/x.duckdb'}\n",
		"dotdot shared path":     "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: ../x.duckdb}\n",
		"dotdot via subdir":      "id: " + testID + "\nconnections:\n  - {name: a, type: duckdb, path: a/../../x.duckdb}\n",
	}
	for name, doc := range cases {
		_, err := ParseConfig([]byte(doc))
		assert.Error(t, err, name)
	}
	_, err := ParseConfig([]byte("name: x\n"))
	assert.ErrorIs(t, err, ErrMissingID)
}

func TestValidateConnectionName(t *testing.T) {
	for _, ok := range []string{"a", "_x", "prod_pg", "a1_b2", strings.Repeat("a", 63)} {
		assert.NoError(t, ValidateConnectionName(ok), ok)
	}
	for _, bad := range []string{"", "1a", "Prod", "a-b", "a b", strings.Repeat("a", 64), "system", "temp", "memory", "main", `a"; DROP`} {
		assert.Error(t, ValidateConnectionName(bad), bad)
	}
}

func TestMarshalConfig_GoldenAndRoundTrip(t *testing.T) {
	c := Config{
		Version: 1, ID: testID, Name: "Acme Analytics", DefaultConnection: "local", Sandbox: "read-only", AskBeforeWrites: Bool(true),
		Connections: []ConnectionDef{
			{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "analytics", Credentials: "keychain", Options: map[string]string{"sslmode": "require"}},
			{Name: "lake", Type: "s3", Region: "us-east-1", Scope: "s3://acme-lake/"},
			{Name: "local", Type: "duckdb", Path: "data/local.duckdb", ReadOnly: Bool(false)},
		},
	}
	got, err := MarshalConfig(c)
	require.NoError(t, err)
	path := filepath.Join("testdata", "vibe.golden.yaml")
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))

	parsed, err := ParseConfig(got)
	require.NoError(t, err)
	again, err := MarshalConfig(parsed)
	require.NoError(t, err)
	assert.Equal(t, string(got), string(again), "parse -> marshal is byte-stable")
}

func TestInterpolate(t *testing.T) {
	env := func(m map[string]string) LookupFunc {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	got, err := Interpolate("host-${A}-${B}", env(map[string]string{"A": "1", "B": ""}))
	require.NoError(t, err)
	assert.Equal(t, "host-1-", got, "an empty value is set")

	_, err = Interpolate("${MISSING}", env(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MISSING")

	got, err = Interpolate("cost $$5 and $PLAIN and ${A}", env(map[string]string{"A": "x"}))
	require.NoError(t, err)
	assert.Equal(t, "cost $5 and $PLAIN and x", got)
}

func TestResolve_InterpolatesOnlyInOwnScopesAndOnlyNonSecretFields(t *testing.T) {
	env := func(k string) (string, bool) {
		return map[string]string{"HOST": "db.internal", "BAD": "postgres://u:pw@x/y", "NAME": "evil"}[k], k == "HOST" || k == "BAD" || k == "NAME"
	}
	d := ConnectionDef{Name: "pg", Type: "postgres", Host: "${HOST}", Database: "${HOST}", Options: map[string]string{"application_name": "v-${HOST}"}}
	for _, scope := range []Scope{ScopeLocal, ScopeUser} {
		r, err := d.Resolve(env, scope)
		require.NoError(t, err, scope)
		assert.Equal(t, "db.internal", r.Host)
		assert.Equal(t, "v-db.internal", r.Options["application_name"])
	}

	// The shared vibe.yaml never interpolates: the reference is refused, so a
	// pulled commit cannot read your environment.
	_, err := d.Resolve(env, ScopeShared)
	assert.Error(t, err)

	// A variable cannot rename the connection or change its sign-in.
	d2 := ConnectionDef{Name: "pg", Type: "postgres", Host: "h", Credentials: "env:PG"}
	r2, err := d2.Resolve(env, ScopeLocal)
	require.NoError(t, err)
	assert.Equal(t, "pg", r2.Name)

	// An expanded value that smuggles a password is refused.
	_, err = ConnectionDef{Name: "pg", Type: "postgres", Host: "${BAD}"}.Resolve(env, ScopeLocal)
	assert.Error(t, err)

	_, err = ConnectionDef{Name: "pg", Type: "postgres", Host: "${NOPE}"}.Resolve(env, ScopeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "pg"`)
	assert.NotContains(t, err.Error(), "db.internal")
}

// Review #109 item 5: a credential-looking variable never lands in a host or
// option, even in your own scopes.
func TestResolve_RefusesSecretNamedVariables(t *testing.T) {
	always := func(string) (string, bool) { return "AKIASECRET", true }
	for _, name := range []string{"AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN", "PGPASSWORD", "API_KEY", "MOTHERDUCK_TOKEN", "DB_PASSWD", "AUTH_HEADER"} {
		d := ConnectionDef{Name: "pg", Type: "postgres", Host: "${" + name + "}.evil.example"}
		assert.Error(t, d.ValidateIn(ScopeLocal), name)
		r, err := d.Resolve(always, ScopeLocal)
		assert.Error(t, err, name)
		assert.NotContains(t, r.Host, "AKIASECRET")
		_, err = ConnectionDef{Name: "pg", Type: "postgres", Host: "h", Options: map[string]string{"x": "${" + name + "}"}}.Resolve(always, ScopeUser)
		assert.Error(t, err, name)
	}
	assert.NoError(t, ConnectionDef{Name: "pg", Type: "postgres", Host: "${PG_HOST}"}.ValidateIn(ScopeLocal))
}

func TestValidateIn_SharedPaths(t *testing.T) {
	ok := ConnectionDef{Name: "a", Type: "duckdb", Path: "data/local.duckdb"}
	assert.NoError(t, ok.ValidateIn(ScopeShared))
	assert.NoError(t, ConnectionDef{Name: "a", Type: "duckdb", Path: "a/../b.duckdb"}.ValidateIn(ScopeShared), "stays inside after cleaning")
	for _, p := range []string{"/etc/x.duckdb", `\\server\share\x`, "C:/x.duckdb", "~/x", "../x", "a/../../x", ".."} {
		assert.Error(t, ConnectionDef{Name: "a", Type: "duckdb", Path: p}.ValidateIn(ScopeShared), p)
	}
	// Your own scopes may use absolute paths.
	for _, sc := range []Scope{ScopeLocal, ScopeUser} {
		assert.NoError(t, ConnectionDef{Name: "a", Type: "duckdb", Path: "/data/x.duckdb"}.ValidateIn(sc))
	}
}

func TestFingerprint(t *testing.T) {
	base := ConnectionDef{Name: "pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "analytics"}
	fp := Fingerprint(base)
	assert.Len(t, fp, 32)
	assert.Equal(t, fp, Fingerprint(base), "stable")

	renamed := base
	renamed.Name = "other"
	renamed.ReadOnly = Bool(false)
	assert.Equal(t, fp, Fingerprint(renamed), "name and read_only are not part of server identity")

	assert.Equal(t, fp, Fingerprint(ConnectionDef{Type: " Postgres ", Host: "DB.internal", Port: 5432, Database: "analytics"}), "case and spaces normalised")

	for name, mut := range map[string]func(*ConnectionDef){
		"host":     func(d *ConnectionDef) { d.Host = "evil.example" },
		"port":     func(d *ConnectionDef) { d.Port = 6432 },
		"database": func(d *ConnectionDef) { d.Database = "other" },
		"type":     func(d *ConnectionDef) { d.Type = "mysql" },
		"scope":    func(d *ConnectionDef) { d.Scope = "s3://b/" },
		"region":   func(d *ConnectionDef) { d.Region = "eu-west-1" },
		"path":     func(d *ConnectionDef) { d.Path = "/x.db" },
		"endpoint": func(d *ConnectionDef) { d.Options = map[string]string{"endpoint": "evil.example"} },
		"sslmode":  func(d *ConnectionDef) { d.Options = map[string]string{"sslmode": "require"} },
	} {
		d := base
		mut(&d)
		assert.NotEqual(t, fp, Fingerprint(d), "%s changes the fingerprint", name)
	}
}

func TestFingerprint_EmptyOptionIsNoOption(t *testing.T) {
	base := ConnectionDef{Type: "s3", Scope: "s3://b", Options: map[string]string{"endpoint": "e"}}
	withEmpty := ConnectionDef{Type: "s3", Scope: "s3://b", Options: map[string]string{"endpoint": "e", "url_style": " "}}
	assert.Equal(t, Fingerprint(base), Fingerprint(withEmpty))
}
