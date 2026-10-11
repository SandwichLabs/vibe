//go:build integration

package connections_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

func TestPostgresTestProbe(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "create table people(id int, name text); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(pg.port)
	def := connections.Def{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", ReadOnly: true, Options: map[string]string{"sslmode": "disable"}}

	res := connections.Test(context.Background(), def, secrets.Credential{User: "postgres", Password: pgPassword},
		connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.GreaterOrEqual(t, res.Count, 1)
	assert.Equal(t, session.StatusOK, res.Status)
	t.Logf("test result: %+v", res)

	bad := connections.Test(context.Background(), def, secrets.Credential{User: "postgres", Password: "WRONGpw_needle_5521"},
		connections.WithTestExtensionDir(extDir(t)))
	require.False(t, bad.OK)
	assert.NotContains(t, bad.Error, "WRONGpw_needle_5521")
	assert.Equal(t, session.StatusNeedsCredentials, bad.Status, bad.Error)
	t.Logf("wrong password: %+v", bad)
}

func connDef(name string, port int, opts map[string]string) connections.Def {
	return connections.Def{Name: name, Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", ReadOnly: true, Options: opts}
}

func testConn(t *testing.T, d connections.Def, c secrets.Credential) connections.TestResult {
	return connections.Test(context.Background(), d, c, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
}
