package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/awssso"
	"github.com/SandwichLabs/assay/internal/secrets"
	"github.com/SandwichLabs/assay/internal/session"
	"github.com/SandwichLabs/assay/internal/testsupport/ssofake"
	"github.com/SandwichLabs/assay/internal/workspace"
)

const ssoStudioConfig = `[profile dev]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = NeedleRoleName

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
`

type evRec struct {
	mu   sync.Mutex
	list []evEntry
}
type evEntry struct {
	Name string
	Data any
}

func (e *evRec) add(name string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var d any
	if len(data) > 0 {
		d = data[0]
	}
	e.list = append(e.list, evEntry{name, d})
}

func (e *evRec) named(name string) []evEntry {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []evEntry
	for _, x := range e.list {
		if x.Name == name {
			out = append(out, x)
		}
	}
	return out
}

func (e *evRec) json() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.Marshal(e.list)
	return string(b)
}

type ssoRig struct {
	app    *App
	fake   *ssofake.Fake
	srv    *httptest.Server
	home   string
	events *evRec
	opened []string
	mu     sync.Mutex
}

func fastSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(min(d, 20*time.Millisecond)):
		return nil
	}
}

// newSSORig opens a workspace with an s3 connection "store" that signs in with
// the fake profile "dev", over a loopback fake Identity Center.
func newSSORig(t *testing.T, fopts ...ssofake.Option) *ssoRig {
	t.Helper()
	r := &ssoRig{fake: ssofake.New(fopts...), home: t.TempDir(), events: &evRec{}}
	r.srv = httptest.NewServer(r.fake)
	t.Cleanup(r.srv.Close)
	t.Setenv("HOME", r.home)
	for _, k := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_PROFILE"} {
		t.Setenv(k, "")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(r.home, ".aws"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(r.home, ".aws", "config"), []byte(ssoStudioConfig), 0o600))
	app := newBareApp(t)
	app.secrets = workspace.NewResolver(
		workspace.WithSSOTestEndpoints(r.srv.URL, r.srv.URL),
		workspace.WithSSOOptions(awssso.WithSleep(fastSleep)),
		workspace.WithSecretsOptions(secrets.WithKeychain(&secrets.Mock{}), secrets.WithRedactor(app.secrets.Redactor())))
	app.emit = r.events.add
	app.openURL = func(u string) { r.mu.Lock(); r.opened = append(r.opened, u); r.mu.Unlock() }
	res, err := app.OpenWorkspace(newWorkspaceDir(t, "SSO Test"))
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://acme-dev/", Region: "us-east-1"}))
	require.NoError(t, app.ws.SetSignIn("store", "aws-sso:dev"))
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "other", Type: "s3", Scope: "s3://acme-other/", Region: "us-east-1"}))
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "pg", Type: "postgres", Host: "db.internal", Database: "d"}))
	app.syncConnections()
	r.app = app
	return r
}

func (r *ssoRig) openedURLs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.opened...)
}

func (r *ssoRig) waitFlows(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { r.app.flows.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a sign-in flow did not end")
	}
}

func (r *ssoRig) cacheFiles(t *testing.T) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(r.home, ".aws", "sso", "cache", "*.json"))
	return m
}

func TestStartSignIn_ShowsCodeOpensPageAndFinishes(t *testing.T) {
	r := newSSORig(t)
	p, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	assert.Equal(t, ssofake.UserCode, p.Code)
	assert.Equal(t, "dev", p.Profile)
	assert.Equal(t, "store", p.Connection)
	assert.True(t, strings.HasPrefix(p.URL, r.srv.URL+"/verify"), p.URL)
	assert.False(t, p.ExpiresAt.IsZero())
	assert.Equal(t, []string{p.URL}, r.openedURLs(), "the page was opened once, by Go, with the checked address")
	assert.Empty(t, r.events.named("signin:done"), "nothing is done before the person approves")

	r.fake.Approve()
	r.waitFlows(t)
	done := r.events.named("signin:done")
	require.Len(t, done, 1)
	assert.Equal(t, map[string]any{"name": "store", "profile": "dev"}, done[0].Data)
	assert.Empty(t, r.events.named("signin:failed"))
	assert.NotEmpty(t, r.events.named("connection:status"), "the connection was preflighted and its status sent")
	assert.Len(t, r.cacheFiles(t), 1, "the shared SSO token cache was written")
	for _, n := range []string{ssofake.NeedleAccess, ssofake.NeedleRefresh, ssofake.NeedleClientSecret, ssofake.NeedleSecretKey, ssofake.NeedleSessionToken} {
		assert.NotContains(t, r.events.json(), n, "events carry no token")
	}
	b, _ := json.Marshal(p)
	assert.NotContains(t, string(b), "NEEDLE")
}

func TestStartSignIn_OneFlowPerProfile(t *testing.T) {
	r := newSSORig(t)
	// A second connection on the same profile joins the running flow.
	require.NoError(t, r.app.ws.SetSignIn("other", "aws-sso:dev"))
	p1, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	p2, err := r.app.StartSignIn(ConnectionRef{Name: "other"})
	require.NoError(t, err)
	assert.Equal(t, p1.Code, p2.Code)
	assert.Equal(t, p1.URL, p2.URL)
	assert.Equal(t, 1, r.fake.Registrations(), "one device flow, not two")
	assert.Len(t, r.openedURLs(), 1)

	r.fake.Approve()
	r.waitFlows(t)
	assert.Len(t, r.events.named("signin:done"), 1)
	// Preflight reached every connection that uses the profile.
	names := map[string]bool{}
	for _, e := range r.events.named("connection:status") {
		if m, ok := e.Data.(map[string]any); ok {
			n, _ := m["name"].(string)
			names[n] = true
		}
	}
	assert.True(t, names["store"] && names["other"], "both connections on the profile were refreshed: %v", names)
}

func TestCancelSignIn_EndsTheFlowQuietly(t *testing.T) {
	r := newSSORig(t)
	_, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	r.app.CancelSignIn(ConnectionRef{Name: "store"})
	r.waitFlows(t)
	assert.Empty(t, r.events.named("signin:done"))
	assert.Empty(t, r.events.named("signin:failed"), "a cancel is not a failure")
	assert.Empty(t, r.cacheFiles(t))
	// And a new flow can start.
	_, err = r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	assert.Equal(t, 2, r.fake.Registrations())
	r.app.CancelSignIn(ConnectionRef{Name: "store"})
	r.waitFlows(t)
}

func TestSignIn_FlowsEndWithTheWorkspace(t *testing.T) {
	r := newSSORig(t)
	_, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	require.NoError(t, r.app.CloseWorkspace())
	r.waitFlows(t)
	assert.Empty(t, r.events.named("signin:done"))
	assert.Empty(t, r.events.named("signin:failed"))
	assert.Empty(t, r.cacheFiles(t))
}

func TestStartSignIn_AHostileAddressIsNeverOpened(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example/device",
		"https://amazonaws.com.evil.example/",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"http://169.254.169.254/latest",
		"https://xn--80ak6aa92e.awsapps.com/start",
		"https://corp.awsapps.com:8443/start",
	} {
		t.Run(bad, func(t *testing.T) {
			r := newSSORig(t, ssofake.WithVerificationURL(bad))
			p, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
			require.Error(t, err)
			assert.Nil(t, p)
			assert.Empty(t, r.openedURLs(), "BrowserOpenURL is never reached with it")
			r.waitFlows(t)
			assert.Empty(t, r.cacheFiles(t))
		})
	}
}

func TestStartSignIn_DeniedIsReportedWithARedactedSentence(t *testing.T) {
	r := newSSORig(t)
	_, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	r.fake.Deny()
	r.waitFlows(t)
	fail := r.events.named("signin:failed")
	require.Len(t, fail, 1)
	m, ok := fail[0].Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "store", m["name"])
	assert.Contains(t, m["message"], "denied")
	assert.Empty(t, r.events.named("signin:done"))
	assert.NotContains(t, r.events.json(), "NEEDLE")
}

func TestStartSignIn_RefusesWhatIsNotAnAWSSignIn(t *testing.T) {
	r := newSSORig(t)
	_, err := r.app.StartSignIn(ConnectionRef{Name: "pg"})
	require.ErrorIs(t, err, workspace.ErrNotSSOSignIn)
	_, err = r.app.StartSignIn(ConnectionRef{Name: "nope"})
	require.Error(t, err)
	_, err = newBareApp(t).StartSignIn(ConnectionRef{Name: "store"})
	require.ErrorIs(t, err, errNoWorkspace)
	assert.Empty(t, r.openedURLs())
	assert.Zero(t, r.fake.Registrations())
}

// hintSession reports the one status the hub gives a signed-out SSO connection
// (whether the s3 extension is installed on the machine running the test or not).
type hintSession struct{ session.Session }

func (h hintSession) Connections() []session.Status {
	return []session.Status{{Name: "store", Type: "s3", Kind: session.StatusNeedsCredentials,
		Message: "AWS session for profile dev has expired", SignIn: &secrets.SignInHint{Kind: "aws-sso", Subject: "dev", Reason: "expired"}}}
}

func TestConnectionInfo_CarriesTheSignInHintAndNothingElse(t *testing.T) {
	r := newSSORig(t)
	r.app.sess = hintSession{r.app.sess}
	list, err := r.app.ListConnections()
	require.NoError(t, err)
	var got *ConnectionInfo
	for i := range list.Connections {
		if list.Connections[i].Name == "store" {
			got = &list.Connections[i]
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, "needs-credentials", got.Status)
	require.NotNil(t, got.SignInNeeded)
	assert.Equal(t, secrets.SignInHint{Kind: "aws-sso", Subject: "dev", Reason: "expired"}, *got.SignInNeeded)
	b, _ := json.Marshal(list)
	for _, n := range []string{"123456789012", "NeedleRoleName", "corp.awsapps.com"} {
		assert.NotContains(t, string(b), n, "no account, role or start URL reaches the window")
	}
}

// Two buttons (banner and rail, banner and editor card) can start the same
// profile at the same moment: the join must not race the flow's own reads.
func TestStartSignIn_ConcurrentJoinsShareOneFlowWithoutARace(t *testing.T) {
	r := newSSORig(t)
	require.NoError(t, r.app.ws.SetSignIn("other", "aws-sso:dev"))
	var wg sync.WaitGroup
	prompts := make([]*SignInPrompt, 8)
	for i := range prompts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "store"
			if i%2 == 1 {
				name = "other"
			}
			p, err := r.app.StartSignIn(ConnectionRef{Name: name})
			assert.NoError(t, err)
			prompts[i] = p
		}()
	}
	wg.Wait()
	for _, p := range prompts {
		require.NotNil(t, p)
		assert.Equal(t, prompts[0].Code, p.Code)
	}
	assert.Equal(t, 1, r.fake.Registrations())
	r.fake.Approve()
	r.waitFlows(t)
	done := r.events.named("signin:done")
	require.Len(t, done, 1)
	m, ok := done[0].Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, prompts[0].Connection, m["name"], "the name is the connection that started the flow")
}

func TestCancelSignIn_ThenSignInStartsANewFlow(t *testing.T) {
	r := newSSORig(t)
	_, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	r.app.CancelSignIn(ConnectionRef{Name: "store"})
	_, err = r.app.StartSignIn(ConnectionRef{Name: "store"}) // no wait: must not join the cancelled flow
	require.NoError(t, err)
	assert.Equal(t, 2, r.fake.Registrations())
	r.fake.Approve()
	r.waitFlows(t)
	assert.Len(t, r.events.named("signin:done"), 1)
}

// The needle audit of #131 step 9 over the Studio surface: after a complete
// sign-in (device flow, token cache, Preflight with role keys from the portal)
// no binding result, event, status, workspace file or config file carries a
// token, the client secret or a role key, nor the profile's account, role or
// start URL. The token cache is the one place the tokens are written.
func TestSignIn_NoNeedleReachesABindingEventOrFolder(t *testing.T) {
	r := newSSORig(t)
	needles := []string{ssofake.NeedleAccess, ssofake.NeedleRefresh, ssofake.NeedleClientSecret, ssofake.NeedleSecretKey,
		ssofake.NeedleSessionToken, "123456789012", "NeedleRoleName", "corp.awsapps.com"}
	var seen []string
	collect := func(v any, err error) {
		if err != nil {
			seen = append(seen, err.Error())
		}
		b, _ := json.Marshal(v)
		seen = append(seen, string(b))
	}
	surfaces := func() {
		collect(r.app.ListConnections())
		collect(r.app.GetConnection("store"))
		collect(r.app.GetConnectionCredentialStatus("store"))
		collect(r.app.TestConnection(TestConnectionRequest{Scope: "local", SignIn: "aws-sso:dev",
			Def: workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://acme-dev/", Region: "us-east-1"}}))
	}
	surfaces() // signed out
	p, err := r.app.StartSignIn(ConnectionRef{Name: "store"})
	require.NoError(t, err)
	collect(p, nil)
	r.fake.Approve()
	r.waitFlows(t)
	require.Len(t, r.events.named("signin:done"), 1)
	surfaces() // signed in, keys resolved
	require.Positive(t, r.fake.PortalHits(), "the role keys were fetched, so their needles were exercised")
	for _, st := range r.app.sess.Connections() {
		if st.Name == "store" {
			assert.NotEqual(t, session.StatusNeedsCredentials, st.Kind, "signed in: %s", st.Message)
		}
	}

	all := strings.Join(seen, "\n") + r.events.json() + strings.Join(r.openedURLs(), "\n")
	for _, n := range needles {
		assert.NotContains(t, all, n, "bindings, events and statuses")
	}
	for _, dir := range []string{r.app.ws.Root(), os.Getenv("ASSAY_CONFIG_DIR")} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if b, rerr := os.ReadFile(path); rerr == nil { //nolint:gosec // test walk of its own temp dirs
				for _, n := range needles {
					assert.NotContains(t, string(b), n, path)
				}
			}
			return nil
		}))
	}
	cache := r.cacheFiles(t)
	require.Len(t, cache, 1, "the token cache is where the session lives")
	fi, err := os.Stat(cache[0])
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(filepath.Dir(cache[0]))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}
