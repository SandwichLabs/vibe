package hub

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHolderDecision(t *testing.T) {
	root := t.TempDir()
	studio := &Holder{App: AppStudio, PID: 1, MCP: &MCPEndpoint{URL: "http://127.0.0.1:1/sse"}}
	starting := &Holder{App: AppServe, PID: 2, MCP: &MCPEndpoint{Pending: true}}
	opening := &Holder{App: AppServe, PID: 3}

	proxy, err := holderDecision(root, studio, nil)
	require.NoError(t, err)
	assert.True(t, proxy, "a live holder that serves MCP is proxied to")

	for name, h := range map[string]*Holder{"pending": starting, "serve opening": opening} {
		proxy, err = holderDecision(root, h, nil)
		require.NoError(t, err, name)
		assert.False(t, proxy, name+": wait for the other `mcp serve` to finish opening")
	}

	proxy, err = holderDecision(root, nil, ErrNotHeld)
	require.NoError(t, err)
	assert.False(t, proxy, "a holder that just exited: try to open again")

	_, err = holderDecision(root, nil, errors.New("garbled lock"))
	require.ErrorIs(t, err, ErrHubHeld)
	assert.Contains(t, err.Error(), "cannot be read safely")
}

func TestStartupTimeout(t *testing.T) {
	last := errors.New("held")
	assert.Equal(t, last, startupTimeout(last, nil, ErrNotHeld))
	assert.Equal(t, last, startupTimeout(last, nil, nil))
	err := startupTimeout(last, &Holder{App: AppStudio, PID: 9}, nil)
	require.ErrorIs(t, err, ErrHubHeld)
	assert.Contains(t, err.Error(), "did not finish opening")
}

func TestServeOptionsApplyDefaults(t *testing.T) {
	o := ServeOptions{}
	require.NoError(t, o.applyDefaults())
	assert.Equal(t, "stdio", o.Transport)
	assert.Equal(t, AppServe, o.App)
	assert.NotNil(t, o.Err)
	assert.NotNil(t, o.Logger)

	bad := ServeOptions{Transport: "carrier-pigeon"}
	require.Error(t, bad.applyDefaults())
}
