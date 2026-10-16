package awssso

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Needles: none of these may ever appear in an error, a formatted value or a log.
const (
	needleAccess  = "NEEDLE_ACCESS_TOKEN"
	needleRefresh = "NEEDLE_REFRESH_TOKEN"
	needleClient  = "NEEDLE_CLIENT_SECRET"
	needleKey     = "NEEDLE_ROLE_SECRET_KEY"
	needleSession = "NEEDLE_ROLE_SESSION_TOKEN"
	needleCfg     = "NEEDLE_CONFIG_CONTENT"
)

func allNeedles() []string {
	return []string{needleAccess, needleRefresh, needleClient, needleKey, needleSession, needleCfg}
}

// fakeAWS is a loopback IAM Identity Center: OIDC and portal on one server.
type fakeAWS struct {
	*httptest.Server
	mu sync.Mutex

	deviceCalls []string // grant types seen at /token
	reqs        []recorded
	portalHits  int

	// device authorization
	verification      string // verificationUriComplete; default <server>/verify
	verificationPlain string
	interval          int
	expiresIn         int
	// /token answers for the device grant, consumed in order; "" = success
	tokenSeq []string
	// refresh grant: "" = success, else the error type
	refreshErr string
	// access tokens the portal accepts
	valid map[string]bool
	// portal refuses everything with this status (0 = normal)
	portalStatus int
	portalType   string
	regExpires   int64
}

type recorded struct {
	Path string
	Body map[string]any
}

func newFake(t *testing.T) *fakeAWS {
	t.Helper()
	f := &fakeAWS{valid: map[string]bool{needleAccess: true}, interval: 1, expiresIn: 600, regExpires: time.Now().Add(90 * 24 * time.Hour).Unix()}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAWS) opts() []Option { return []Option{WithTestEndpoints(f.URL, f.URL)} }

func (f *fakeAWS) bodies(path string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, r := range f.reqs {
		if r.Path == path {
			out = append(out, r.Body)
		}
	}
	return out
}

func (f *fakeAWS) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.portalHits
}

func writeJSON(w http.ResponseWriter, code int, errType string, v any) {
	w.Header().Set("Content-Type", "application/json")
	if errType != "" {
		w.Header().Set("X-Amzn-Errortype", errType)
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeAWS) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if r.Body != nil {
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
	}
	f.reqs = append(f.reqs, recorded{Path: r.URL.Path, Body: body})
	switch r.URL.Path {
	case "/client/register":
		writeJSON(w, 200, "", map[string]any{"clientId": "cid-1", "clientSecret": needleClient, "clientSecretExpiresAt": f.regExpires, "clientIdIssuedAt": time.Now().Unix()})
	case "/device_authorization":
		v := f.verification
		if v == "" {
			v = f.URL + "/verify?user_code=ABCD-EFGH"
		}
		resp := map[string]any{"deviceCode": "dev-code", "userCode": "ABCD-EFGH", "verificationUriComplete": v,
			"interval": f.interval, "expiresIn": f.expiresIn}
		if f.verificationPlain != "" {
			resp["verificationUri"] = f.verificationPlain
		}
		writeJSON(w, 200, "", resp)
	case "/token":
		f.token(w, body)
	case "/federation/credentials":
		f.portal(w, r)
	default:
		http.NotFound(w, r)
	}
}

func oidcErr(w http.ResponseWriter, typ, code string) {
	writeJSON(w, 400, typ, map[string]any{"error": code, "error_description": "x"})
}

func (f *fakeAWS) token(w http.ResponseWriter, body map[string]any) {
	grant, _ := body["grantType"].(string)
	f.deviceCalls = append(f.deviceCalls, grant)
	if grant == "refresh_token" {
		if f.refreshErr != "" {
			oidcErr(w, f.refreshErr, "invalid_grant")
			return
		}
		f.valid["REFRESHED_"+needleAccess] = true
		writeJSON(w, 200, "", map[string]any{"accessToken": "REFRESHED_" + needleAccess, "expiresIn": 3600, "refreshToken": needleRefresh + "_2", "tokenType": "Bearer"})
		return
	}
	next := ""
	if len(f.tokenSeq) > 0 {
		next, f.tokenSeq = f.tokenSeq[0], f.tokenSeq[1:]
	}
	if next != "" {
		oidcErr(w, next, "x")
		return
	}
	writeJSON(w, 200, "", map[string]any{"accessToken": needleAccess, "expiresIn": 3600, "refreshToken": needleRefresh, "tokenType": "Bearer"})
}

func (f *fakeAWS) portal(w http.ResponseWriter, r *http.Request) {
	f.portalHits++
	if f.portalStatus != 0 {
		writeJSON(w, f.portalStatus, f.portalType, map[string]any{"message": "x"})
		return
	}
	if !f.valid[r.Header.Get("x-amz-sso_bearer_token")] {
		writeJSON(w, 401, "UnauthorizedException", map[string]any{"message": "session expired " + r.Header.Get("x-amz-sso_bearer_token")})
		return
	}
	writeJSON(w, 200, "", map[string]any{"roleCredentials": map[string]any{"accessKeyId": "ASIAFAKE", "secretAccessKey": needleKey,
		"sessionToken": needleSession, "expiration": time.Now().Add(time.Hour).UnixMilli()}})
}

const sessionCfg = `[profile dev]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = Reader
region = us-east-1

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
sso_registration_scopes = sso:account:access, custom:scope
`

const legacyCfg = `[profile old]
sso_start_url = https://corp.awsapps.com/start
sso_region = eu-west-1
sso_account_id = 123456789012
sso_role_name = Reader
`

// home builds a fake home with ~/.aws/config.
func home(t *testing.T, cfg string) string {
	t.Helper()
	h := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(h, ".aws"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(h, ".aws", "config"), []byte(cfg), 0o600))
	return h
}

func writeCache(t *testing.T, h string, prof Profile, tf tokenFile) string {
	t.Helper()
	path := tokenPath(h, prof.cacheKey())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	b, err := json.Marshal(tf)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))
	return path
}

func validToken() tokenFile {
	return tokenFile{StartURL: "https://corp.awsapps.com/start", Region: "us-east-1", AccessToken: needleAccess,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}

func sessionProf() Profile {
	return Profile{Name: "dev", Kind: KindSession, SessionName: "corp", StartURL: "https://corp.awsapps.com/start", Region: "us-east-1"}
}

func legacyProf() Profile {
	return Profile{Name: "old", Kind: KindLegacy, StartURL: "https://corp.awsapps.com/start", Region: "eu-west-1"}
}

// noLeak fails when any needle appears in s.
func noLeak(t *testing.T, what, s string) {
	t.Helper()
	for _, n := range allNeedles() {
		require.NotContains(t, s, n, what)
	}
}

func fileMode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	require.NoError(t, err)
	return st.Mode().Perm()
}

func cacheJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}
