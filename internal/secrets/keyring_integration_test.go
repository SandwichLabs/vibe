//go:build keyring

package secrets

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Real OS keychain (Linux: Secret Service). Run:
//
//	go test -tags keyring -count=1 -v ./internal/secrets -run Keyring
//
// It uses a throwaway service name and removes everything it creates.
func TestKeyringIntegrationFullCycle(t *testing.T) {
	svc := fmt.Sprintf("vibe-test-114-%d", os.Getpid())
	store := NewKeyringStore(WithKeyringService(svc))
	ctx := context.Background()

	if err := store.Probe(ctx); err != nil {
		t.Skipf("no usable keychain here: %v", err)
	}

	const pw = "INTEGRATION-NEEDLE-pw-9d41"
	wsRef := WorkspaceRef("ws-int", "prod_pg")
	llmRef := AppRef("llm-test")
	t.Cleanup(func() {
		_ = store.Delete(ctx, wsRef)
		_ = store.Delete(ctx, llmRef)
	})

	var logs bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	red := NewRedactor()
	r := NewResolver(WithKeychain(store), WithRedactor(red), WithResolverLogger(lg))
	b := Binding{Fingerprint: "fp1", Server: "db.example:5432"}

	_, err := r.Resolve(ctx, wsRef, "keychain", b)
	require.Error(t, err)

	require.NoError(t, r.SetConnectionCredentials(ctx, wsRef, "keychain", Credential{User: "rosa", Password: pw}, b))
	c, err := r.Resolve(ctx, wsRef, "keychain", b)
	require.NoError(t, err)
	assert.Equal(t, pw, c.Password)

	st := r.GetConnectionCredentialStatus(ctx, wsRef, "keychain", b)
	assert.True(t, st.SecretSet)
	assert.Equal(t, "rosa", st.Username)
	assert.NotContains(t, fmt.Sprintf("%+v", st), pw)

	_, err = r.Resolve(ctx, wsRef, "keychain", Binding{Fingerprint: "fp2", Server: "other:5432"})
	var fc *ErrFingerprintChanged
	require.ErrorAs(t, err, &fc)

	lg.Info("connect failed", "err", red.Redact(fmt.Sprintf("password='%s' rejected", pw)))

	require.NoError(t, store.Set(ctx, llmRef, []byte("llm-NEEDLE-key")))
	got, err := store.Get(ctx, llmRef)
	require.NoError(t, err)
	assert.Equal(t, "llm-NEEDLE-key", string(got))

	require.NoError(t, r.ClearConnectionCredentials(ctx, wsRef))
	require.NoError(t, store.Delete(ctx, llmRef))
	_, err = store.Get(ctx, wsRef)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = store.Get(ctx, llmRef)
	assert.ErrorIs(t, err, ErrNotFound)

	assert.NotContains(t, logs.String(), "NEEDLE", "no secret value in the logs of a full cycle")
	t.Logf("cycle ok on service %s; log bytes %d", svc, logs.Len())
}
