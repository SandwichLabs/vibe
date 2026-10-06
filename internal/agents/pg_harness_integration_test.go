//go:build integration

package agents_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const pgPassword = "S3cretPW_agent_7731"

type pgServer struct {
	id   string
	port string
}

func containerBin() string {
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	return "podman"
}

// extDir is the extension cache shared by integration runs (never ~/.duckdb).
func extDir(t testing.TB) string {
	d := filepath.Join(os.TempDir(), "vibe-conn-it-ext")
	require.NoError(t, os.MkdirAll(d, 0o755))
	return d
}

func startPG(t testing.TB) *pgServer {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d",
		"-e", "POSTGRES_PASSWORD="+pgPassword,
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine", "-c", "log_statement=all").CombinedOutput()
	require.NoError(t, err, string(out))
	id := strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	s := &pgServer{id: id}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "5432/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	s.port = line[strings.LastIndex(line, ":")+1:]
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
			return s
		}
	}
	t.Fatalf("postgres never became ready")
	return nil
}

func (s *pgServer) psql(t testing.TB, sqlText string) string {
	t.Helper()
	out, err := exec.Command(containerBin(), "exec", "-e", "PGPASSWORD="+pgPassword, s.id,
		"psql", "-h", "127.0.0.1", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-tAc", sqlText).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (s *pgServer) logs() string {
	out, _ := exec.Command(containerBin(), "logs", s.id).CombinedOutput()
	return string(out)
}
