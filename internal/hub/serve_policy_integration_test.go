//go:build unix && integration

package hub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hubPGPassword = "S3cretPW_hub_7731"

func startHubPG(t *testing.T) (id, port string) {
	t.Helper()
	bin := "docker"
	if p, err := exec.LookPath("docker"); err == nil {
		bin = p
	} else if p, err := exec.LookPath("podman"); err == nil {
		bin = p
	}
	out, err := exec.Command(bin, "run", "--rm", "-d", "-e", "POSTGRES_PASSWORD="+hubPGPassword,
		"-p", "127.0.0.1::5432", "postgres:16-alpine").CombinedOutput()
	require.NoError(t, err, string(out))
	id = strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "5432/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	port = line[strings.LastIndex(line, ":")+1:]
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ok := 0
		for i := 0; i < 2; i++ {
			if exec.Command(bin, "exec", id, "psql", "-h", "127.0.0.1", "-U", "postgres", "-tAc", "select 1").Run() == nil {
				ok++
			}
			time.Sleep(500 * time.Millisecond)
		}
		if ok == 2 {
			return id, port
		}
	}
	t.Fatal("postgres never became ready")
	return "", ""
}

func hubPSQL(t *testing.T, id, q string) string {
	t.Helper()
	bin := "docker"
	if p, err := exec.LookPath("docker"); err == nil {
		bin = p
	}
	out, err := exec.Command(bin, "exec", "-e", "PGPASSWORD="+hubPGPassword, id, "psql", "-h", "127.0.0.1", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-tAc", q).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// TestServeProxyApprovesRemoteWritesThroughStudio is the in-Studio MCP approval
// path (#116): Studio owns the workspace and serves MCP with its approver, a
// headless `vibe mcp serve` proxies to it, and a write to a Postgres connection
// is asked about even though this person turned "Ask before changes" off.
func TestServeProxyApprovesRemoteWritesThroughStudio(t *testing.T) {
	id, portStr := startHubPG(t)
	hubPSQL(t, id, "create table people(id int primary key, name text); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(portStr)

	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	res, err := workspace.Init(filepath.Join(t.TempDir(), "acme"), workspace.WithInitLogger(quietLogger()))
	require.NoError(t, err)
	require.NoError(t, res.Workspace.Close())
	r := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()), secrets.WithRedactor(secrets.NewRedactor()))
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithLogger(quietLogger()), workspace.WithConfigDir(os.Getenv("VIBE_CONFIG_DIR")),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	f := false
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", ReadOnly: &f,
		Options: map[string]string{"sslmode": "disable"}}))
	require.NoError(t, ws.SetCredentials(t.Context(), "pg", secrets.Credential{User: "postgres", Password: hubPGPassword}, false))
	// The person turned the gate off: a local write would not ask.
	local := ws.Local()
	local.Agent = workspace.LocalAgent{Sandbox: workspace.SandboxReadWrite, AskBeforeWrites: workspace.Bool(false)}
	require.NoError(t, ws.SaveLocal(local))

	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	ext := filepath.Join(os.TempDir(), "vibe-conn-it-ext")
	sess, issues, err := workspace.OpenSession(t.Context(), ws, quietLogger(), workspace.WithSessionOptions(session.WithExtensionDirectory(ext)))
	require.NoError(t, err)
	require.Empty(t, issues)
	defer sess.Close() //nolint:errcheck
	studio, err := StartWorkspaceMCP(ws, sess, hold, WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	defer studio.Stop() //nolint:errcheck

	var mu sync.Mutex
	var asked []agents.ApprovalRequest
	decision := agents.ErrApprovalDeclined
	studio.Handler.SetApprover(func(_ context.Context, req agents.ApprovalRequest) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, req)
		return decision
	})

	run := startServe(t, ServeOptions{Workspace: ws.Root()})
	run.handshake()

	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM pg.public.people"})
	require.False(t, isErr, text)

	// Local write with the gate off: no card.
	text, isErr = run.tool("run_query", map[string]any{"sql": "CREATE TABLE IF NOT EXISTS pg.public.x (a int)"})
	assert.True(t, isErr, "read-write cannot run DDL: %s", text)

	// Remote write: asked even with the gate off. Declined first.
	text, isErr = run.tool("run_query", map[string]any{"sql": "INSERT INTO pg.public.people VALUES (3, 'cy')"})
	require.True(t, isErr, text)
	assert.Contains(t, text, "declined")
	mu.Lock()
	require.Len(t, asked, 1)
	assert.Equal(t, []string{"pg"}, asked[0].Connections)
	decision = nil
	mu.Unlock()
	assert.Equal(t, "2", hubPSQL(t, id, "select count(*) from people"))

	// Approved: the row lands in Postgres.
	text, isErr = run.tool("run_query", map[string]any{"sql": "INSERT INTO pg.public.people VALUES (3, 'cy')"})
	require.False(t, isErr, text)
	assert.Equal(t, "3", hubPSQL(t, id, "select count(*) from people"))

	// The never-list is not approvable: no card is even shown.
	mu.Lock()
	before := len(asked)
	mu.Unlock()
	text, isErr = run.tool("run_query", map[string]any{"sql": "CALL postgres_execute('pg', 'DROP TABLE people')"})
	require.True(t, isErr)
	assert.NotContains(t, text, hubPGPassword)
	mu.Lock()
	assert.Equal(t, before, len(asked), "a never-list statement never reaches an approver")
	mu.Unlock()
	assert.Equal(t, "3", hubPSQL(t, id, "select count(*) from people"))
}
