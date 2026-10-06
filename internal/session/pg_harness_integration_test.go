//go:build integration

package session

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const pgPassword = "S3cretPW_spike_7731"

// pgServer is a throwaway postgres container.
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

func startPG(t *testing.T) *pgServer {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d",
		"-e", "POSTGRES_PASSWORD="+pgPassword,
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
		"-c", "log_statement=all", "-c", "log_min_messages=info").CombinedOutput()
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
		// Real readiness: a TCP-visible query (initdb restarts the server once).
		if err := exec.Command(bin, "exec", id, "psql", "-h", "127.0.0.1", "-U", "postgres", "-tAc", "select 1").Run(); err == nil {
			time.Sleep(500 * time.Millisecond)
			if err := exec.Command(bin, "exec", id, "psql", "-h", "127.0.0.1", "-U", "postgres", "-tAc", "select 1").Run(); err == nil {
				return s
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres never became ready")
	return nil
}

// psql runs SQL inside the container as postgres.
func (s *pgServer) psql(t testing.TB, sqlText string) string {
	t.Helper()
	out, err := exec.Command(containerBin(), "exec", "-e", "PGPASSWORD="+pgPassword, s.id,
		"psql", "-h", "127.0.0.1", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-tAc", sqlText).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (s *pgServer) logs(t testing.TB) string {
	out, _ := exec.Command(containerBin(), "logs", s.id).CombinedOutput()
	return string(out)
}

// openWithPostgres opens a hardened hub (the plan's §4.4 DSN) with the
// postgres extension installed + loaded. Extensions are cached across runs.
func openWithPostgres(t *testing.T, extra string) *sql.DB {
	return openWithPostgresOpts(t, extra, true)
}

// openWithPostgresOpts: harden=false leaves allow_persistent_secrets at its default.
func openWithPostgresOpts(t *testing.T, extra string, harden bool) *sql.DB {
	t.Helper()
	extDir := filepath.Join(os.TempDir(), "vibe-spike-ext")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	q := url.Values{}
	if harden {
		q.Set("allow_persistent_secrets", "false")
	}
	q.Set("autoinstall_known_extensions", "false")
	q.Set("autoload_known_extensions", "false")
	q.Set("allow_community_extensions", "false")
	q.Set("extension_directory", extDir)
	dsn := "?" + q.Encode()
	if extra != "" {
		dsn += "&" + extra
	}
	db := openDSN(t, dsn)
	db.SetMaxOpenConns(1)
	_, err := db.Exec("INSTALL postgres")
	require.NoError(t, err, "INSTALL postgres (needs network)")
	_, err = db.Exec("LOAD postgres")
	require.NoError(t, err)
	return db
}

func (s *pgServer) createSecret(db *sql.DB, name, password string) error {
	_, err := db.ExecContext(context.Background(), fmt.Sprintf(
		"CREATE OR REPLACE TEMPORARY SECRET %s (TYPE postgres, HOST '127.0.0.1', PORT %s, DATABASE 'postgres', USER 'postgres', PASSWORD '%s')",
		name, s.port, password))
	return err
}
