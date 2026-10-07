//go:build integration

package connections_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const myPassword = "S3cretPW_mysql_4417"

type mysqlServer struct {
	id   string
	port string
}

// startMySQL runs a throwaway mysql:8 and waits until it answers on TCP (the
// image's init phase runs a temporary server that listens on a socket only).
func startMySQL(t testing.TB) *mysqlServer {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d",
		"-e", "MYSQL_ROOT_PASSWORD="+myPassword,
		"-e", "MYSQL_DATABASE=app",
		"-p", "127.0.0.1::3306",
		"mysql:8").CombinedOutput()
	require.NoError(t, err, string(out))
	id := strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	s := &mysqlServer{id: id}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "3306/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	s.port = line[strings.LastIndex(line, ":")+1:]
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		ok := 0
		for i := 0; i < 2; i++ {
			if exec.Command(bin, "exec", "-e", "MYSQL_PWD="+myPassword, id, "mysql", "-h", "127.0.0.1", "-uroot", "-N", "-e", "select 1").Run() == nil {
				ok++
			}
			time.Sleep(700 * time.Millisecond)
		}
		if ok == 2 {
			return s
		}
	}
	t.Fatalf("mysql never became ready")
	return nil
}

// mysql runs one statement as root in the app database and returns its output.
func (s *mysqlServer) mysql(t testing.TB, sqlText string) string {
	t.Helper()
	out, err := exec.Command(containerBin(), "exec", "-e", "MYSQL_PWD="+myPassword, s.id,
		"mysql", "-h", "127.0.0.1", "-uroot", "-N", "-B", "app", "-e", sqlText).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}
