//go:build keyring

package session

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

const kSvc = "vibe-spike-107"

// Spike #107: go-keyring against the Secret Service on this machine.
// Run: go test -tags keyring ./internal/session -run Keyring -v
func TestSpikeKeyringRoundTrip(t *testing.T) {
	user := "conn-" + t.Name()
	t.Cleanup(func() { _ = keyring.Delete(kSvc, user) })

	_, err := keyring.Get(kSvc, user)
	require.ErrorIs(t, err, keyring.ErrNotFound)

	t0 := time.Now()
	require.NoError(t, keyring.Set(kSvc, user, "pw-1"))
	t.Logf("Set: %v", time.Since(t0).Round(time.Millisecond))
	t0 = time.Now()
	got, err := keyring.Get(kSvc, user)
	require.NoError(t, err)
	assert.Equal(t, "pw-1", got)
	t.Logf("Get: %v", time.Since(t0).Round(time.Millisecond))

	// Overwrite replaces (no duplicate item).
	require.NoError(t, keyring.Set(kSvc, user, "pw-2"))
	got, _ = keyring.Get(kSvc, user)
	assert.Equal(t, "pw-2", got)

	// Odd payloads: multiline, unicode, a 3 KiB token.
	for name, v := range map[string]string{
		"multiline": "line1\nline2\n",
		"unicode":   "pässwörd-日本語-🔑",
		"long":      strings.Repeat("x", 3072),
		"quote":     `a'b"c$d\e`,
	} {
		require.NoError(t, keyring.Set(kSvc, user, v), name)
		got, err = keyring.Get(kSvc, user)
		require.NoError(t, err, name)
		assert.Equal(t, v, got, name)
	}

	require.NoError(t, keyring.Delete(kSvc, user))
	_, err = keyring.Get(kSvc, user)
	require.ErrorIs(t, err, keyring.ErrNotFound)
	err = keyring.Delete(kSvc, user)
	t.Logf("Delete of missing item: %v (ErrNotFound=%v)", err, errors.Is(err, keyring.ErrNotFound))
}

// Helper process: run Set/Get under whatever env the parent gave it.
func TestSpikeHelperKeyring(t *testing.T) {
	if os.Getenv("SPIKE_KEYRING_HELPER") == "" {
		t.Skip("helper process only")
	}
	user := "conn-helper"
	t0 := time.Now()
	err := keyring.Set(kSvc, user, "pw-helper")
	os.Stdout.WriteString("SET_ERR=" + errString(err) + " in " + time.Since(t0).Round(time.Millisecond).String() + "\n")
	t0 = time.Now()
	_, err = keyring.Get(kSvc, user)
	os.Stdout.WriteString("GET_ERR=" + errString(err) + " in " + time.Since(t0).Round(time.Millisecond).String() + "\n")
	_ = keyring.Delete(kSvc, user)
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// Simulate SSH/headless shells by removing session-bus env from the child.
func TestSpikeKeyringWithoutSessionBus(t *testing.T) {
	scenarios := map[string][]string{
		"BOGUS bus address (no reachable bus)":   {"DBUS_SESSION_BUS_ADDRESS", "BOGUS"},
		"baseline (env intact)":                  nil,
		"DBUS_SESSION_BUS_ADDRESS unset":         {"DBUS_SESSION_BUS_ADDRESS"},
		"DBUS + XDG_RUNTIME_DIR unset (ssh-ish)": {"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"},
		"DISPLAY/WAYLAND also unset":             {"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "DISPLAY", "WAYLAND_DISPLAY"},
	}
	for name, drop := range scenarios {
		t.Run(name, func(t *testing.T) {
			env := []string{}
			for _, e := range os.Environ() {
				k := e[:strings.IndexByte(e, '=')]
				skip := false
				for _, d := range drop {
					if k == d {
						skip = true
					}
				}
				if !skip {
					env = append(env, e)
				}
			}
			env = append(env, "SPIKE_KEYRING_HELPER=1")
			if len(drop) == 2 && drop[1] == "BOGUS" {
				env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus", "XDG_RUNTIME_DIR=/nonexistent")
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestSpikeHelperKeyring$", "-test.v")
			cmd.Env = env
			done := make(chan struct{})
			var out []byte
			var err error
			go func() { out, err = cmd.CombinedOutput(); close(done) }()
			select {
			case <-done:
			case <-time.After(60 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("keyring call hung >60s without a session bus")
			}
			for _, l := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(l, "SET_ERR") || strings.HasPrefix(l, "GET_ERR") {
					t.Logf("%s", l)
				}
			}
			_ = err
		})
	}
}
