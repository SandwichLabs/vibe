package connections

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

func reqFloci() Def {
	return Def{Name: "floci", Type: "s3", Scope: "s3://lake/", Region: "us-east-1",
		Options: map[string]string{"endpoint": "127.0.0.1:1", "url_style": "path", "use_ssl": "false"}}
}

func reqLake() Def {
	return Def{Name: "sales", Type: "ducklake", Path: "s3://lake/cat.ducklake", ReadOnly: true, Options: map[string]string{"data_path": "s3://lake/data/"}}
}

func TestRequiredStorageWithoutASecretIsNeverAttemptedAndTheLakeIsNot(t *testing.T) {
	res := Test(context.Background(), reqLake(), secrets.Credential{}, emptyExt(t),
		WithRequiredStorage(reqFloci(), secrets.Credential{}))
	require.False(t, res.OK)
	assert.Equal(t, "floci", res.Requirement)
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
	assert.Equal(t, CodeNeedsCredentials, res.Code)
	assert.Equal(t, "No credentials are saved for floci on this computer. Open it to sign in.", res.Message)
	assert.NotContains(t, res.Detail, "catalog in object storage", "the lake's URL short-circuit message means it was never read")
}

func TestRequiredStorageFailureIsTheStoragesAndRedacted(t *testing.T) {
	// No httpfs in an empty extension directory: the storage part fails first.
	cred := secrets.Credential{KeyID: "AKIAneedle_id", Secret: "storage_secret_needle"}
	red := secrets.NewRedactor()
	res := Test(context.Background(), reqLake(), secrets.Credential{}, emptyExt(t),
		WithRequiredStorage(reqFloci(), cred), WithTestRedactor(red))
	require.False(t, res.OK)
	assert.Equal(t, "floci", res.Requirement)
	assert.Equal(t, session.StatusExtensionMissing, res.Status)
	assert.Equal(t, CodeExtensionMissing, res.Code)
	for _, s := range []string{res.Error, res.Message, res.Detail} {
		assert.NotContains(t, s, "storage_secret_needle")
		assert.NotContains(t, s, "AKIAneedle_id")
	}
	assert.NotContains(t, red.Redact("storage_secret_needle"), "storage_secret_needle", "the caller's redactor learned the storage credential")
}

func TestRequiredStorageCannotBeTheConnectionUnderTest(t *testing.T) {
	d := reqFloci()
	res := Test(context.Background(), d, secrets.Credential{KeyID: "k", Secret: "s"}, emptyExt(t),
		WithRequiredStorage(d, secrets.Credential{KeyID: "k", Secret: "s"}))
	require.False(t, res.OK)
	assert.Equal(t, "floci", res.Requirement)
	assert.Contains(t, res.Error, "cannot be the connection under test")
}

func TestRequiredStorageMustBeAStorageConnection(t *testing.T) {
	res := Test(context.Background(), reqLake(), secrets.Credential{}, emptyExt(t),
		WithRequiredStorage(Def{Name: "pg", Type: "postgres", Host: "h", Database: "d"}, secrets.Credential{User: "u", Password: "p"}))
	require.False(t, res.OK)
	assert.Equal(t, "pg", res.Requirement)
	assert.Contains(t, res.Error, "not a storage connection")
}

func TestWithoutACoveringStorageSecretAURLIsNeverRead(t *testing.T) {
	res := Test(context.Background(), reqLake(), secrets.Credential{}, emptyExt(t))
	require.True(t, res.OK, res.Error)
	assert.Contains(t, res.Detail, "object storage")
	assert.Empty(t, res.Requirement)
}

func TestRequiredStorageThatDoesNotCoverTheLakeIsRefusedBeforeAnythingIsAttached(t *testing.T) {
	cred := secrets.Credential{KeyID: "k", Secret: "s"}
	for name, store := range map[string]Def{
		"wrong bucket": func() Def { d := reqFloci(); d.Scope = "s3://other/"; return d }(),
		"wrong scheme": {Name: "gcsstore", Type: "gcs", Scope: "gcs://lake/"},
		"narrower":     func() Def { d := reqFloci(); d.Scope = "s3://lake/data/"; return d }(),
	} {
		t.Run(name, func(t *testing.T) {
			// A tiny timeout and an empty extension dir: reaching the attach would
			// show as extension-missing or a timeout, not this refusal.
			res := Test(context.Background(), reqLake(), secrets.Credential{}, emptyExt(t), WithRequiredStorage(store, cred))
			require.False(t, res.OK)
			assert.Equal(t, session.StatusError, res.Status)
			assert.Contains(t, res.Error, "nothing was read")
			assert.Empty(t, res.Requirement)
		})
	}
}
