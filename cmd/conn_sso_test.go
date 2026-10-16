package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

const ssoCLIConfig = `[profile dev]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = NeedleRoleName

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
`

// ssoHome points HOME at a folder with an SSO profile "dev" and no cached
// token: signed out, no network.
func ssoHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	for _, k := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		t.Setenv(k, "")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(h, ".aws"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(h, ".aws", "config"), []byte(ssoCLIConfig), 0o600))
}

func connNames(t *testing.T, root string) string {
	t.Helper()
	out, stderr, err := runVibe("conn", "list", "--workspace", root)
	require.NoError(t, err, stderr)
	return out
}

func TestConnAdd_AWSSSOSignIn(t *testing.T) {
	ssoHome(t)
	root := newWS(t)
	out, stderr, err := runVibe("conn", "add", "dev", "--type", "s3", "--prefix", "s3://acme-dev/", "--region", "us-east-1", "--sign-in", "aws-sso:dev", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Added s3 connection dev")
	assert.Contains(t, out, "signs in with your AWS profile dev")
	assert.Contains(t, out, "aws sso login --profile dev")
	assert.Contains(t, out, "Nothing is stored")
	assert.NotContains(t, out, "No terminal", "no key prompt for an SSO sign-in")
	assert.Contains(t, connNames(t, root), "aws-sso:dev")
	// The profile name is per person: never in the committed file.
	assert.NotContains(t, readSharedConfig(t, root), "aws-sso")
}

func TestConnAdd_RefusedSSOSignInLeavesNoConnectionBehind(t *testing.T) {
	ssoHome(t)
	for name, args := range map[string][]string{
		"minio endpoint": {"--type", "s3", "--prefix", "s3://b/", "--option", "endpoint=localhost:9000"},
		"smuggled port":  {"--type", "s3", "--prefix", "s3://b/", "--option", "endpoint=evil.example:443.amazonaws.com"},
		"tls off":        {"--type", "s3", "--prefix", "s3://b/", "--option", "use_ssl=false"},
		"postgres":       {"--type", "postgres", "--host", "db.internal", "--database", "d"},
		"bad profile":    {"--type", "s3", "--prefix", "s3://b/"},
	} {
		root := newWS(t)
		profile := "aws-sso:dev"
		if name == "bad profile" {
			profile = "aws-sso:a b"
		}
		a := append([]string{"conn", "add", "x", "--scope", "shared", "--sign-in", profile, "--workspace", root}, args...)
		_, _, err := runVibe(a...)
		require.Error(t, err, name)
		assert.NotContains(t, readSharedConfig(t, root), "name: x", name+": nothing was saved")
		assert.NotContains(t, connNames(t, root), "x ", name)
	}
}

func TestConnCredentials_AWSSSOStatusAndSetRefuseAKey(t *testing.T) {
	ssoHome(t)
	root := newWS(t)
	_, stderr, err := runVibe("conn", "add", "dev", "--type", "s3", "--prefix", "s3://acme-dev/", "--sign-in", "aws-sso:dev", "--workspace", root)
	require.NoError(t, err, stderr)

	out, stderr, err := runVibe("conn", "credentials", "status", "dev", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "method:        aws-sso:dev")
	assert.Contains(t, out, "profile:       dev")
	assert.Contains(t, out, "signed in:     false")
	assert.Contains(t, out, "aws sso login --profile dev")
	assert.Contains(t, out, "secret set:    false")
	for _, n := range []string{"123456789012", "NeedleRoleName", "corp.awsapps.com"} {
		assert.NotContains(t, out, n)
	}

	// `credentials set` asks for nothing and stores nothing.
	out, stderr, err = runVibe("conn", "credentials", "set", "dev", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Nothing is stored")
}

func TestConnTest_AWSSSOWithoutASessionSaysSignIn(t *testing.T) {
	ssoHome(t)
	root := newWS(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, stderr, err := runVibe("conn", "add", "dev", "--type", "s3", "--prefix", "s3://acme-dev/", "--sign-in", "aws-sso:dev", "--workspace", root)
	require.NoError(t, err, stderr)
	_, _, err = runVibe("conn", "test", "dev", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs credentials")
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
}

func TestCLIResolverKnowsAWSSSO(t *testing.T) {
	ssoHome(t)
	st := cliResolver().Status(context.Background(), secrets.Ref{Name: "x"}, "aws-sso:dev", secrets.Binding{})
	assert.True(t, st.Available, st.Detail)
	assert.Equal(t, "dev", st.Profile)
}
