//go:build integration

package connections_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const minioSecret = "minioSecret_needle1"

// startMinIO runs a throwaway S3-compatible server with a bucket "lake".
func startMinIO(t *testing.T) string {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d", "--entrypoint", "sh",
		"-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD="+minioSecret,
		"-p", "127.0.0.1::9000", "docker.io/minio/minio",
		"-c", "mkdir -p /data/lake && exec minio server /data").CombinedOutput()
	if err != nil {
		t.Skipf("minio is not available here: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "9000/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	ep := "127.0.0.1:" + line[strings.LastIndex(line, ":")+1:]
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("curl", "-s", "-o", "/dev/null", "-m", "2", "http://"+ep+"/minio/health/ready").Run() == nil {
			return ep
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("minio never became ready")
	return ""
}

func TestS3ConnectUseAndWrongKeys(t *testing.T) {
	ep := startMinIO(t)
	ctx := context.Background()
	d := connections.Def{Name: "lake", Type: "s3", Scope: "s3://lake/raw/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}
	good := secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}

	// Test: right keys, then wrong keys (a 403 reads needs-credentials, redacted).
	res := connections.Test(ctx, d, good, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	t.Logf("test ok: %+v", res)
	bad := connections.Test(ctx, d, secrets.Credential{KeyID: "minioadmin", Secret: "WRONG_needle2"}, connections.WithTestExtensionDir(extDir(t)))
	require.False(t, bad.OK)
	assert.Equal(t, session.StatusNeedsCredentials, bad.Status, bad.Error)
	assert.NotContains(t, bad.Error, "WRONG_needle2")

	// Through a workspace: the secret exists at open, so a URL read works on
	// first use with no explicit Connect.
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "lake", Type: "s3", Scope: d.Scope, Region: d.Region, Options: d.Options}))
	require.NoError(t, ws.SetCredentials(ctx, "lake", good, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	require.Empty(t, issues)
	assert.Equal(t, session.StatusOK, connStatus(sess, "lake").Kind, "the storage secret is made at open: it dials nothing")

	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "COPY (SELECT 1 AS x UNION ALL SELECT 2) TO 's3://lake/raw/a.parquet'", nil)
	require.NoError(t, err)
	rs, err := eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/raw/a.parquet')", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, rs.Rows[0][0])

	// New (wrong) keys: Preflight replaces the secret and reads stop working.
	require.NoError(t, ws.SetCredentials(ctx, "lake", secrets.Credential{KeyID: "minioadmin", Secret: "WRONG_needle3"}, false))
	sess.Preflight(ctx, "lake")
	_, err = eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/raw/a.parquet')", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "WRONG_needle3")
	// A cleared credential drops the secret and reports needs-credentials.
	require.NoError(t, ws.ClearCredentials(ctx, "lake"))
	sess.Preflight(ctx, "lake")
	assert.Equal(t, session.StatusNeedsCredentials, connStatus(sess, "lake").Kind)
}

// A Delta location that sorts before its storage connection is opened with the
// storage connection already registered: it is not "needs the ... connection"
// at open, and once the storage credential is cleared and set again the table
// stops waiting on its own.
func TestDeltaTableOpensBeforeItsStorageSortsFirst(t *testing.T) {
	ep := startMinIO(t)
	ctx := context.Background()
	inst, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(extDir(t)))
	require.NoError(t, err)
	for _, e := range []string{"httpfs", "delta"} {
		require.NoError(t, inst.InstallExtension(ctx, e))
	}
	_ = inst.Close()

	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "events", Type: "delta", Path: "s3://lake/raw/t/"}))
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "zstore", Type: "s3", Scope: "s3://lake/raw/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	good := secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}
	require.NoError(t, ws.SetCredentials(ctx, "zstore", good, false))
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	assert.Equal(t, session.StatusOK, connStatus(sess, "zstore").Kind)
	assert.NotContains(t, connStatus(sess, "events").Message, "needs the", "the storage connection was registered and connected first (there is no table there, so the error is the read's)")

	require.NoError(t, ws.ClearCredentials(ctx, "zstore"))
	sess.Preflight(ctx, "zstore")
	sess.Preflight(ctx, "events")
	assert.Equal(t, `needs the "zstore" connection: it needs credentials`, connStatus(sess, "events").Message)
	require.NoError(t, ws.SetCredentials(ctx, "zstore", good, false))
	sess.Preflight(ctx, "zstore")
	assert.NotContains(t, connStatus(sess, "events").Message, "needs the", "no manual action after signing in")
}
