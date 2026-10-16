//go:build unix && integration

package hub

import (
	"context"
	"crypto/sha1" //nolint:gosec // the AWS CLI names its token cache by SHA-1
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/testsupport/miniotest"
	"github.com/SandwichLabs/vibe/internal/testsupport/ssofake"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// These tests are the acceptance of #131 step 6: an s3 connection signed in
// with a (fake) AWS SSO profile connects to MinIO with STS credentials a fake
// portal mints, reads a DuckLake and a wildcard, and every secret stays out of
// statuses, errors, agent results, files and logs. Nothing leaves the machine.

const ssoHubConfig = `[profile dev]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = NeedleRoleName

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
`

type stsCreds struct {
	KeyID, Secret, Token string
	Expires              time.Time
}

// assumeRole mints MinIO STS credentials with the root keys (SigV4, plain HTTP).
func assumeRole(t *testing.T, ep string) stsCreds {
	t.Helper()
	form := url.Values{"Action": {"AssumeRole"}, "Version": {"2011-06-15"}, "DurationSeconds": {"900"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, "http://"+ep+"/", strings.NewReader(form))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sum := sha256.Sum256([]byte(form))
	require.NoError(t, v4.NewSigner().SignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: miniotest.KeyID, SecretAccessKey: miniotest.Secret}, req, hex.EncodeToString(sum[:]), "sts", "us-east-1", time.Now()))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, 200, resp.StatusCode, string(body))
	var out struct {
		Credentials struct {
			AccessKeyID     string `xml:"AccessKeyId"`
			SecretAccessKey string
			SessionToken    string
			Expiration      time.Time
		} `xml:"AssumeRoleResult>Credentials"`
	}
	require.NoError(t, xml.Unmarshal(body, &out), string(body))
	c := out.Credentials
	require.NotEmpty(t, c.AccessKeyID)
	require.NotEmpty(t, c.SessionToken, "STS credentials carry a session token")
	return stsCreds{c.AccessKeyID, c.SecretAccessKey, c.SessionToken, c.Expiration}
}

// ssoWorld is a fake home with an SSO profile, a fake portal that hands out
// MinIO STS credentials, and a resolver wired to it.
type ssoWorld struct {
	home   string
	fake   *ssofake.Fake
	portal *httptest.Server
	hits   portalHits
	sts    atomic.Value // stsCreds
	res    *secrets.Resolver
	red    *secrets.Redactor
}

func newSSOWorld(t *testing.T, ep string, signedIn bool) *ssoWorld {
	t.Helper()
	w := &ssoWorld{home: t.TempDir(), red: secrets.NewRedactor()}
	t.Setenv("HOME", w.home)
	for _, k := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(k, "")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".aws"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".aws", "config"), []byte(ssoHubConfig), 0o600))
	if ep != "" {
		w.sts.Store(assumeRole(t, ep))
	}
	w.fake = ssofake.New(ssofake.WithCredentials(func() ssofake.Creds {
		c, _ := w.sts.Load().(stsCreds)
		return ssofake.Creds{KeyID: c.KeyID, Secret: c.Secret, Token: c.Token, Expires: c.Expires}
	}))
	w.hits = portalHits{w.fake}
	w.portal = httptest.NewServer(w.fake)
	t.Cleanup(w.portal.Close)
	w.res = workspace.NewResolver(workspace.WithSSOTestEndpoints(w.portal.URL, w.portal.URL),
		workspace.WithSecretsOptions(secrets.WithKeychain(secrets.NewMemoryStore()), secrets.WithRedactor(w.red)))
	if signedIn {
		w.signIn(t)
	}
	return w
}

// portalHits counts the portal's role-credential calls, as an atomic did.
type portalHits struct{ f *ssofake.Fake }

func (h portalHits) Load() int64 { return int64(h.f.PortalHits()) }

func (w *ssoWorld) cachePath() string {
	sum := sha1.Sum([]byte("corp")) //nolint:gosec // see import
	return filepath.Join(w.home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json")
}

func (w *ssoWorld) signIn(t *testing.T) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(w.cachePath()), 0o700))
	b, _ := json.Marshal(map[string]string{"startUrl": "https://corp.awsapps.com/start", "region": "us-east-1",
		"accessToken": w.fake.IssueToken(), "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	require.NoError(t, os.WriteFile(w.cachePath(), b, 0o600))
}

func (w *ssoWorld) signOut(t *testing.T) { t.Helper(); require.NoError(t, os.Remove(w.cachePath())) }

// ssoNeedles are the values no output may contain.
func (w *ssoWorld) needles() []string {
	c, _ := w.sts.Load().(stsCreds)
	return []string{c.Secret, c.Token, ssofake.NeedleAccess, ssofake.NeedleRefresh, ssofake.NeedleClientSecret, miniotest.Secret, "NeedleRoleName", "123456789012", "corp.awsapps.com"}
}

func noNeedles(t *testing.T, what, s string, needles []string) {
	t.Helper()
	for _, n := range needles {
		if n != "" {
			assert.NotContains(t, s, n, what)
		}
	}
}

func auditDirs(t *testing.T, needles []string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		_ = filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Size() > 64<<20 {
				return nil
			}
			if b, rerr := os.ReadFile(p); rerr == nil {
				noNeedles(t, p, string(b), needles)
			}
			return nil
		})
	}
}

func TestSSOSignedInS3ReadsALakeAndAWildcardFromMinIO(t *testing.T) {
	ep := miniotest.Start(t)
	miniotest.BuildLake(t, ep)
	miniotest.UseExtensionCache(t)
	ctx := context.Background()

	w := newSSOWorld(t, ep, true)
	root, cfg := flociWorkspace(t, ep, "aws-sso", w.res)
	logs := &lockedBuf{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	o, err := Open(ctx, root, OpenOptions{App: "test", Logger: logger, Resolver: w.res, Redactor: w.red})
	require.NoError(t, err)
	defer o.Close()
	require.Empty(t, o.Issues)
	var seen []string

	// The method was saved per person, not in the committed file.
	c, ok := o.WS.Connection("floci")
	require.True(t, ok)
	assert.Equal(t, "aws-sso:dev", c.SignIn)
	b, err := os.ReadFile(filepath.Join(root, "vibe.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(b), "aws-sso")

	// 1. The storage connection is ready (Preflight made its secret, with the
	// STS session token and region written explicitly), and the lake reads
	// through it, with the portal's credentials and nothing anonymous.
	assert.Equal(t, session.StatusOK, statusLine(o.Sess, "floci").Kind, statusLine(o.Sess, "floci").Message)
	rs, err := o.Eng.Execute(ctx, "SELECT count(*) FROM lake.warehouse.dim_students", nil)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(miniotest.Students), fmt.Sprint(rs.Rows[0][0]))
	assert.GreaterOrEqual(t, w.hits.Load(), int64(1))
	assert.Equal(t, session.StatusOK, statusLine(o.Sess, "lake").Kind)

	// 2. A wildcard read: the lister resolves the connection's credential from
	// the provider's cache for each listing; it costs no portal call.
	before := w.hits.Load()
	rs, err = o.Eng.Execute(ctx, "SELECT count(*) > 0 FROM read_parquet('s3://"+miniotest.Bucket+"/warehouse/ducklake/**/*.parquet')", nil)
	require.NoError(t, err)
	assert.Equal(t, "true", fmt.Sprint(rs.Rows[0][0]))
	assert.Equal(t, before, w.hits.Load(), "listing uses the cached role credentials")

	// 3. The agent surface, read-only: reads inside the scope; the secret
	// surfaces, the SSO cache and the host's AWS_* stay out of reach.
	h, _, err := NewAgentHandler(o.WS, o.Sess, "", logger)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(miniotest.Students), fmt.Sprint(rowCount(t, agentCall(h, "SELECT count(*) FROM lake.warehouse.dim_students"))))
	for _, q := range []string{
		"SELECT * FROM duckdb_secrets()",
		"SELECT * FROM which_secret('s3://" + miniotest.Bucket + "/x', 's3')",
		"SELECT getenv('AWS_SECRET_ACCESS_KEY')",
		"SELECT * FROM read_text('" + w.home + "/.aws/sso/cache/*.json')",
		"SELECT * FROM read_text('~/.aws/sso/cache/*.json')",
		"SELECT * FROM read_text('" + w.home + "/.aws/config')",
		"SELECT * FROM read_parquet('s3://other-bucket/x.parquet')",
	} {
		resp := agentCall(h, q)
		require.NotNil(t, resp.Error, q)
		seen = append(seen, resp.Error.Message)
	}

	// 3b. URL parameters cannot send the SSO session token to another host (#138).
	other, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer other.Close() //nolint:errcheck
	var strangerHits atomic.Int64
	go func() {
		for {
			c, aerr := other.Accept()
			if aerr != nil {
				return
			}
			strangerHits.Add(1)
			_ = c.Close()
		}
	}()
	rs, err = o.Eng.Execute(ctx, "SELECT current_setting('s3_url_compatibility_mode')", nil)
	require.NoError(t, err)
	assert.Equal(t, "true", fmt.Sprint(rs.Rows[0][0]))
	_, err = o.Eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://"+miniotest.Bucket+"/warehouse/ducklake/x.parquet?s3_endpoint="+other.Addr().String()+"&s3_use_ssl=false&s3_url_style=path')", nil)
	require.Error(t, err)
	seen = append(seen, err.Error())
	assert.Equal(t, int64(0), strangerHits.Load(), "nothing reached the other host")

	// 4. Audit: statuses, errors, results, logs, files.
	for _, st := range o.Sess.Connections() {
		j, _ := json.Marshal(st)
		seen = append(seen, string(j))
	}
	needles := w.needles()
	all, _ := json.Marshal(seen)
	noNeedles(t, "statuses and errors", string(all), needles)
	noNeedles(t, "logs", logs.String(), needles)
	auditDirs(t, needles, root, cfg)
	assert.Contains(t, logs.String(), "level=DEBUG", "the audit ran with debug logging on")
}

func TestSSOSignedOutNeedsSignInEverywhereAndNeverReadsAnonymously(t *testing.T) {
	ep := miniotest.Start(t)
	miniotest.BuildLake(t, ep)
	miniotest.UseExtensionCache(t)
	ctx := context.Background()

	w := newSSOWorld(t, ep, true)
	root, cfg := flociWorkspace(t, ep, "aws-sso", w.res)
	w.signOut(t)

	logs := &lockedBuf{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	o, err := Open(ctx, root, OpenOptions{App: "test", Logger: logger, Resolver: w.res, Redactor: w.red})
	require.NoError(t, err)
	defer o.Close()
	require.Empty(t, o.Issues)
	assert.Equal(t, int64(0), w.hits.Load(), "a signed-out profile never calls the portal, at open or later")

	st := statusLine(o.Sess, "floci")
	assert.Equal(t, session.StatusNeedsCredentials, st.Kind, st.Message)
	require.NotNil(t, st.SignIn)
	assert.Equal(t, secrets.SignInHint{Kind: "aws-sso", Subject: "dev", Reason: "missing"}, *st.SignIn)
	assert.Contains(t, st.Message, "aws sso login --profile dev")
	var seen []string
	seen = append(seen, st.Message)

	// The lake says what it waits on.
	err = o.Sess.Connect(ctx, "lake")
	require.Error(t, err)
	assert.Equal(t, `needs the "floci" connection: it needs credentials`, statusLine(o.Sess, "lake").Message)
	seen = append(seen, err.Error())

	// The wildcard lister fails with the sign-in hint: no empty or anonymous
	// secret is built, and nothing is read.
	_, err = o.Eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://"+miniotest.Bucket+"/warehouse/ducklake/**/*.parquet')", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aws sso login --profile dev", "the lister says how to sign in")
	seen = append(seen, err.Error())
	// Same for a literal read of an object that was never read before.
	_, err = o.Eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://"+miniotest.Bucket+"/warehouse/ducklake/nope.parquet')", nil)
	require.Error(t, err)
	seen = append(seen, err.Error())

	// Test says the same, without dialling.
	c, _ := o.WS.Connection("floci")
	rc, err := workspace.ResolveConnection(o.WS, c, nil)
	require.NoError(t, err)
	_, err = workspace.TestCredential(ctx, o.WS, rc, "", secrets.Credential{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
	seen = append(seen, err.Error())

	// The person signs in elsewhere (aws sso login); Preflight picks it up.
	w.signIn(t)
	o.Sess.Preflight(ctx, "floci")
	assert.Equal(t, session.StatusOK, statusLine(o.Sess, "floci").Kind, statusLine(o.Sess, "floci").Message)
	assert.Nil(t, statusLine(o.Sess, "floci").SignIn)
	rs, err := o.Eng.Execute(ctx, "SELECT count(*) FROM lake.warehouse.dim_students", nil)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(miniotest.Students), fmt.Sprint(rs.Rows[0][0]))

	needles := w.needles()
	all, _ := json.Marshal(seen)
	noNeedles(t, "errors", string(all), needles)
	noNeedles(t, "logs", logs.String(), needles)
	auditDirs(t, needles, root, cfg)
}

// `vibe mcp serve` with an expired fake session: run_query answers with the
// sign-in hint, at once, with no browser and no sign-in flow.
func TestSSOMCPServeReportsNeedsSignIn(t *testing.T) {
	miniotest.UseExtensionCache(t)
	w := newSSOWorld(t, "", false) // signed out: no token cache at all
	cfg := t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", cfg)
	res, err := workspace.Init(filepath.Join(t.TempDir(), "acme"), workspace.WithInitLogger(quietLogger()))
	require.NoError(t, err)
	require.NoError(t, res.Workspace.Close())
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithLogger(quietLogger()), workspace.WithConfigDir(cfg),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(workspace.NewResolver()))
	require.NoError(t, err)
	// An AWS endpoint (none), so the production endpoint rule applies.
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "dev", Type: "s3", Scope: "s3://acme-dev/", Region: "us-east-1"}))
	require.NoError(t, ws.SetSignIn("dev", "aws-sso:dev"))
	require.NoError(t, ws.Close())

	run := startServe(t, ServeOptions{Workspace: res.Workspace.Root()})
	run.handshake()
	start := time.Now()
	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT * FROM read_parquet('s3://acme-dev/x.parquet')"})
	assert.Less(t, time.Since(start), 5*time.Second, "refuses at once: no hang, no waiting for a browser")
	assert.True(t, isErr, text)
	assert.Contains(t, text, "no ready storage connection", "the agent's URL policy stops it before any read: the connection is not ready")
	assert.Contains(t, text, `the storage connection "dev" covers it but is not ready`, "the refusal names the connection that covers the URL")
	assert.Contains(t, text, "aws sso login --profile dev", "and how to sign in")
	conns, cerr := run.tool("list_connections", map[string]any{})
	require.False(t, cerr, conns)
	assert.Contains(t, conns, `"status":"needs-credentials"`, "the client is told the connection needs sign-in")
	assert.Contains(t, conns, `"sign_in":{"kind":"aws-sso","subject":"dev","reason":"missing"}`, "list_connections carries the typed hint")
	for _, s := range []string{text, conns, run.stderr.String()} {
		noNeedles(t, "mcp output", s, w.needles())
	}
	assert.Equal(t, int64(0), w.hits.Load())
	_, statErr := os.Stat(filepath.Join(w.home, ".aws", "sso"))
	assert.True(t, os.IsNotExist(statErr), "serve wrote no token cache: it only reads one")
	text, isErr = run.tool("run_query", map[string]any{"sql": "SELECT 41 + 1"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "42", "a statement that needs no storage still runs")
}
