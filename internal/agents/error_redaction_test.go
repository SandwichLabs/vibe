package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// What reaches the agent or an MCP client never carries the connection's user
// name (the UI may show it to the person who owns the connection).
func TestErrorResponsesHideTheConnectionUserName(t *testing.T) {
	r := NewErrorResponse(1, ErrInternal, `failed to attach "pg": Unable to connect to Postgres at user='rosa' password='***' host='db'`)
	assert.NotContains(t, r.Error.Message, "rosa")
	assert.Contains(t, r.Error.Message, "host='db'")
}

func TestWithConnectErrorPrefersTheConnectionsOwnProblem(t *testing.T) {
	denial, cerr := assertErr("denied"), assertErr(`failed to connect "pg": no credentials`)
	assert.Equal(t, cerr, withConnectError(denial, cerr))
	assert.Equal(t, denial, withConnectError(denial, nil))
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
