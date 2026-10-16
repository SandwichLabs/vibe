package awssso

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProfileBothFormats(t *testing.T) {
	ctx := context.Background()
	p := New(home(t, sessionCfg+"\n"+legacyCfg), nil)

	s, err := p.LoadProfile(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, KindSession, s.Kind)
	assert.Equal(t, "corp", s.SessionName)
	assert.Equal(t, "https://corp.awsapps.com/start", s.StartURL)
	assert.Equal(t, "us-east-1", s.Region)
	assert.Equal(t, []string{"sso:account:access", "custom:scope"}, s.Scopes)

	l, err := p.LoadProfile(ctx, "old")
	require.NoError(t, err)
	assert.Equal(t, KindLegacy, l.Kind)
	assert.Equal(t, "eu-west-1", l.Region)
	assert.Equal(t, "123456789012", l.AccountID)
	assert.Empty(t, l.Scopes)
}

func TestLoadProfileRefusesAnythingButSSO(t *testing.T) {
	cfg := `[profile static]
aws_access_key_id = AKIAxxxx
aws_secret_access_key = ` + needleCfg + `
[profile proc]
credential_process = /bin/echo ` + needleCfg + `
[profile role]
role_arn = arn:aws:iam::123456789012:role/x
source_profile = dev
[profile mixed]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = Reader
credential_process = /bin/echo ` + needleCfg + `
[profile mixedrole]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
role_arn = arn:aws:iam::123456789012:role/x
[profile nosession]
sso_session = ghost
sso_account_id = 123456789012
sso_role_name = Reader
[profile disagree]
sso_session = corp
sso_start_url = https://other.awsapps.com/start
sso_account_id = 123456789012
sso_role_name = Reader
[profile badregion]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1.evil.example
sso_account_id = 123456789012
sso_role_name = Reader
[profile httpstart]
sso_start_url = http://corp.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile customhost]
sso_start_url = https://sso.corp.example.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile evilsuffix]
sso_start_url = https://corp.awsapps.com.evil.example/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile wrongportal]
sso_start_url = https://ssoins-1.portal.eu-west-1.app.aws/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile portalport]
sso_start_url = https://corp.awsapps.com:8443/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile upper]
sso_start_url = https://CORP.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile okportal]
sso_start_url = https://ssoins-1.portal.us-east-1.app.aws/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile userinfo]
sso_start_url = https://u:p@corp.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Reader
[profile badacct]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
sso_account_id = 12
sso_role_name = Reader
[profile partial]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
` + sessionCfg
	h := home(t, cfg)
	p := New(h, nil)
	for _, name := range []string{"static", "proc", "role", "mixed", "mixedrole", "nosession", "disagree", "badregion", "httpstart", "customhost", "evilsuffix", "wrongportal", "portalport", "upper", "userinfo", "badacct", "partial", "missing", "dev/../x", "-x", ""} {
		_, err := p.LoadProfile(context.Background(), name)
		require.Error(t, err, name)
		var pe *ProfileError
		require.ErrorAs(t, err, &pe, name)
		noLeak(t, name, err.Error())
		assert.NotContains(t, err.Error(), h, "no path in the message")
		assert.NotContains(t, err.Error(), "AKIA")
	}
	ok, err := p.LoadProfile(context.Background(), "okportal")
	require.NoError(t, err)
	assert.Equal(t, KindLegacy, ok.Kind)
	_, err = p.LoadProfile(context.Background(), "mixed")
	assert.Contains(t, err.Error(), "credential_process")
	assert.Contains(t, err.Error(), "mixed")
}

func TestConfigFilesComeFromTheSnapshotOnly(t *testing.T) {
	h := home(t, "") // the default config is empty
	other := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.WriteFile(other, []byte(sessionCfg), 0o600))
	// AWS_CONFIG_FILE in the process environment is not read: only the getenv passed in is.
	t.Setenv("AWS_CONFIG_FILE", other)
	_, err := New(h, nil).LoadProfile(context.Background(), "dev")
	require.Error(t, err)
	got, err := New(h, func(k string) string {
		if k == "AWS_CONFIG_FILE" {
			return other
		}
		return ""
	}).LoadProfile(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "corp", got.SessionName)
	_, err = New("", nil).LoadProfile(context.Background(), "dev")
	require.Error(t, err, "no home and no AWS_CONFIG_FILE")
}

func TestTokenPathMatchesTheSDKAndTheCLI(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	for _, key := range []string{"corp", "https://corp.awsapps.com/start", "../../etc/passwd"} {
		want, err := ssocreds.StandardCachedTokenFilepath(key)
		require.NoError(t, err)
		assert.Equal(t, want, tokenPath(h, key))
	}
	assert.Equal(t, filepath.Join(h, ".aws", "sso", "cache"), filepath.Dir(tokenPath(h, "../../etc/passwd")), "a crafted name cannot leave the cache folder")
}

func TestProfileAndCredsAreRedactedEverywhere(t *testing.T) {
	c := Creds{KeyID: "ASIAFAKE", Secret: needleKey, Token: needleSession}
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		noLeak(t, f, sprintf(f, c))
		noLeak(t, f+" ptr", sprintf(f, &c))
		noLeak(t, f+" in struct", sprintf(f, struct{ C Creds }{c}))
	}
	assert.NotContains(t, sprintf("%v", c), "ASIAFAKE")
	pr := Profile{Name: "dev", StartURL: "https://corp.awsapps.com/start", AccountID: "123456789012", RoleName: "Reader"}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, form := range []string{sprintf(f, pr), sprintf(f, &pr)} {
			assert.NotContains(t, form, "corp.awsapps.com")
			assert.NotContains(t, form, "123456789012")
			assert.NotContains(t, form, "Reader")
		}
	}
	assert.Equal(t, []string{needleKey, needleSession}, c.SecretValues())
}
