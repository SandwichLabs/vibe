//go:build integration

package connections_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// Azurite's well-known development account.
const (
	azuriteAccount = "devstoreaccount1"
	azuriteKey     = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
)

type azurite struct{ port string }

func startAzurite(t testing.TB) *azurite {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d", "-p", "127.0.0.1::10000",
		"mcr.microsoft.com/azure-storage/azurite", "azurite-blob", "--blobHost", "0.0.0.0", "--skipApiVersionCheck", "--loose").CombinedOutput()
	if err != nil {
		t.Skipf("Azurite is not available here: %v %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "10000/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	a := &azurite{port: line[strings.LastIndex(line, ":")+1:]}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://127.0.0.1:" + a.port + "/"); err == nil {
			_ = resp.Body.Close()
			return a
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("azurite never became ready")
	return nil
}

func (a *azurite) connString() string {
	return fmt.Sprintf("DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=%s;BlobEndpoint=http://127.0.0.1:%s/%s;", azuriteAccount, azuriteKey, a.port, azuriteAccount)
}

// do signs and sends one request with the shared key. path is "container" or
// "container/blob"; query is the canonical resource's query line ("restype:container").
func (a *azurite) do(t testing.TB, path, rawQuery, canonQuery string, body []byte, extra map[string]string) {
	t.Helper()
	date := time.Now().UTC().Format(http.TimeFormat)
	const ver = "2021-08-06"
	hdr := map[string]string{"x-ms-date": date, "x-ms-version": ver}
	for k, v := range extra {
		hdr[k] = v
	}
	var names []string
	for k := range hdr {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders string
	for _, k := range names {
		if strings.HasPrefix(k, "x-ms-") {
			canonHeaders += k + ":" + hdr[k] + "\n"
		}
	}
	length := ""
	if len(body) > 0 {
		length = strconv.Itoa(len(body))
	}
	canonRes := "/" + azuriteAccount + "/" + azuriteAccount + "/" + path
	if canonQuery != "" {
		canonRes += "\n" + canonQuery
	}
	sts := "PUT\n\n\n" + length + "\n\n" + extra["Content-Type"] + "\n\n\n\n\n\n\n" + canonHeaders + canonRes
	key, _ := base64.StdEncoding.DecodeString(azuriteKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sts))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	url := "http://127.0.0.1:" + a.port + "/" + azuriteAccount + "/" + path
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	require.NoError(t, err)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "SharedKey "+azuriteAccount+":"+sig)
	req.ContentLength = int64(len(body))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Less(t, resp.StatusCode, 300, "PUT %s: %s", path, resp.Status)
}

func (a *azurite) createContainer(t testing.TB, name string) {
	t.Helper()
	a.do(t, name, "restype=container", "restype:container", nil, nil)
}

func (a *azurite) putBlob(t testing.TB, container, name string, body []byte) {
	t.Helper()
	a.do(t, container+"/"+name, "", "", body, map[string]string{"x-ms-blob-type": "BlockBlob", "Content-Type": "application/octet-stream"})
}

// Azure against Azurite: sign in with a connection string, Test, write and read
// Parquet under the scope, a wrong key is needs-credentials and redacted, and
// nothing secret is in DuckDB's secret listing.
func TestAzureAgainstAzurite(t *testing.T) {
	az := startAzurite(t)
	az.createContainer(t, "events")
	ctx := context.Background()

	def := connections.Def{Name: "blob", Type: "azure", Scope: "az://events/2024/", Options: map[string]string{"sign_in": "connection_string"}}
	res := connections.Test(ctx, def, secrets.Credential{Secret: az.connString()}, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, 0, res.Count, "an empty scope lists no objects")
	assert.NotContains(t, res.Error+res.Detail, azuriteKey)

	bad := strings.Replace(az.connString(), "AccountKey="+azuriteKey, "AccountKey="+base64.StdEncoding.EncodeToString([]byte("not-the-right-key-needle-7788")), 1)
	res = connections.Test(ctx, def, secrets.Credential{Secret: bad}, connections.WithTestExtensionDir(extDir(t)))
	require.False(t, res.OK)
	assert.Equal(t, session.StatusNeedsCredentials, res.Status, res.Error)
	assert.NotContains(t, res.Error, "not-the-right-key-needle-7788")
	assert.NotContains(t, res.Error, base64.StdEncoding.EncodeToString([]byte("not-the-right-key-needle-7788")))
	t.Logf("wrong key: %s", res.Error)

	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "blob", Type: "azure", Scope: "az://events/2024/", Options: map[string]string{"sign_in": "connection_string"}}))
	require.NoError(t, ws.SetCredentials(ctx, "blob", secrets.Credential{Secret: az.connString()}, false))
	fresh := t.TempDir()
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	assert.Equal(t, session.StatusExtensionMissing, connStatus(sess, "blob").Kind)
	require.NoError(t, sess.InstallExtension(ctx, "azure"))
	sess.Preflight(ctx, "blob")
	assert.Equal(t, session.StatusOK, connStatus(sess, "blob").Kind, "a storage connection comes up once it is ready: creating a secret dials nothing")
	defer sess.Close()

	eng := engine.NewManager(sess)
	// Put a Parquet file there the way an uploader would (the extension writes
	// Azure too since DuckDB 1.5; the agent's policy still refuses writes inside a scope).
	local := filepath.Join(t.TempDir(), "people.parquet")
	_, err = sess.DB().Exec("COPY (SELECT * FROM (VALUES (1, 'ada'), (2, 'bob')) t(id, name)) TO '" + local + "' (FORMAT PARQUET)")
	require.NoError(t, err)
	body, err := os.ReadFile(local)
	require.NoError(t, err)
	az.putBlob(t, "events", "2024/people.parquet", body)
	rows := queryAll(t, eng, "SELECT name FROM read_parquet('az://events/2024/people.parquet') ORDER BY id")
	require.Len(t, rows, 2)
	assert.Equal(t, "ada", rows[0][0])
	rows = queryAll(t, eng, "SELECT count(*) FROM glob('az://events/2024/*')")
	assert.EqualValues(t, 1, rows[0][0])

	// The secret listing never shows the key.
	rows = queryAll(t, eng, "SELECT name, secret_string FROM duckdb_secrets() WHERE name = 'vibe_blob'")
	require.Len(t, rows, 1)
	assert.NotContains(t, fmt.Sprint(rows[0]...), azuriteKey)
	assert.Contains(t, fmt.Sprint(rows[0]...), "redacted")
	rows = queryAll(t, eng, "SELECT count(*) FROM duckdb_databases() WHERE database_name = 'blob'")
	assert.EqualValues(t, 0, rows[0][0], "azure attaches no catalog")

	// The agent reads inside the scope (the policy's URL rule extends to Azure)
	// and nowhere else.
	h := agents.NewHandler(workspace.NewAgentAccess(ws, sess), eng,
		&agents.SandboxConfig{Mode: agents.ModeReadOnly, MaxQueryRows: 100, RequireApproval: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run := func(q string) *agents.Response {
		return h.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": q}})
	}
	resp := run("SELECT name FROM read_parquet('az://events/2024/people.parquet') ORDER BY id")
	require.Nil(t, resp.Error, fmt.Sprint(resp.Error))
	resp = run("SELECT count(*) FROM glob('az://events/2024/*')")
	require.Nil(t, resp.Error, fmt.Sprint(resp.Error))
	for _, q := range []string{
		"SELECT * FROM read_parquet('az://events/2023/people.parquet')",
		"SELECT * FROM read_parquet('az://events/2024')",
		"SELECT * FROM read_parquet('az://events-evil/2024/people.parquet')",
		"SELECT * FROM read_parquet('azure://events/2024/people.parquet')",
		"SELECT * FROM read_parquet('abfss://events@acct.dfs.core.windows.net/2024/people.parquet')",
		"SELECT * FROM read_parquet('az://events/2024/../2023/people.parquet')",
		"SELECT * FROM read_parquet('az://events/2024/people.parquet?sv=1')",
		"SELECT * FROM delta_scan('az://events/2023/t')",
		"SELECT * FROM read_parquet('az://events/2024/' || 'people.parquet')",
		"SELECT * FROM duckdb_secrets()",
	} {
		resp := run(q)
		require.NotNil(t, resp.Error, q)
		assert.NotContains(t, resp.Error.Message, azuriteKey, q)
	}
	// Writing is refused in the read-only tier. The extension itself writes Azure
	// since DuckDB 1.5, so in the full tier it is the policy that refuses a COPY
	// into a connection's scope, whatever the approval.
	resp = run("COPY (SELECT 1) TO 'az://events/2024/x.parquet'")
	require.NotNil(t, resp.Error)
	full := agents.NewHandler(workspace.NewAgentAccess(ws, sess), eng,
		&agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100, RequireApproval: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp = full.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": "COPY (SELECT 1) TO 'az://events/2024/x.parquet'"}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "may only write to a plain file path inside the workspace's data/ folder")
	rows = queryAll(t, eng, "SELECT count(*) FROM glob('az://events/2024/x.parquet')")
	assert.EqualValues(t, 0, rows[0][0], "nothing was written")
}

// The service principal and account key variants build secrets DuckDB accepts
// (the sign-in itself needs Azure, so the failure is a network or auth error,
// never a malformed secret).
func TestAzureSecretVariantsAreAcceptedByDuckDB(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		def  connections.Def
		cred secrets.Credential
	}{
		"service principal": {
			connections.Def{Name: "sp", Type: "azure", Scope: "abfss://c@acctname.dfs.core.windows.net/p/", Options: map[string]string{"sign_in": "service_principal", "account_name": "acctname", "tenant_id": "00000000-0000-0000-0000-000000000001"}},
			secrets.Credential{KeyID: "00000000-0000-0000-0000-000000000002", Secret: "client-secret-needle-1"},
		},
		"account key": {
			connections.Def{Name: "ak", Type: "azure", Scope: "azure://c/p/", Options: map[string]string{"sign_in": "account_key", "account_name": "acctname"}},
			secrets.Credential{Secret: "Zm9vYmFyYmF6cXV4a2V5LW5lZWRsZS0y"},
		},
	} {
		res := connections.Test(ctx, tc.def, tc.cred, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)), connections.WithTestTimeout(5*time.Second))
		assert.False(t, res.OK, name)
		assert.NotContains(t, res.Error, "failed to create secret", name)
		assert.NotContains(t, res.Error, tc.cred.Secret, name)
		t.Logf("%s: %s", name, firstLineOf(res.Error))
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
