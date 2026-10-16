package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func addStore(t *testing.T, app *App, name, scope string) {
	t.Helper()
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: name, Type: "s3", Scope: scope, Region: "us-east-1"}))
	app.syncConnections()
}

func TestFindStorageFor_MatchSuggestAndProblem(t *testing.T) {
	app, _ := connApp(t)
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://lake/catalog.sqlite"}})
	require.NoError(t, err)
	require.Len(t, got.Needs, 1)
	assert.Nil(t, got.Needs[0].Match)
	require.NotNil(t, got.Needs[0].Suggest)
	assert.Equal(t, "s3://lake/", got.Needs[0].Suggest.Scope)
	assert.Equal(t, 1, got.Needs[0].Group)

	addStore(t, app, "floci", "s3://lake/")
	got, err = app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://lake/catalog.sqlite", "s3://lake/../x", ""}})
	require.NoError(t, err)
	require.Len(t, got.Needs, 3)
	require.NotNil(t, got.Needs[0].Match)
	assert.Equal(t, "floci", got.Needs[0].Match.Name)
	assert.Equal(t, "local", string(got.Needs[0].Match.In))
	assert.NotEmpty(t, got.Needs[0].Match.Status)
	assert.NotEmpty(t, got.Needs[1].Problem)
	assert.Nil(t, got.Needs[1].Suggest)
	assert.Equal(t, "no URL", got.Needs[2].Problem)
}

func TestFindStorageFor_TemplatedIsNeverExpanded(t *testing.T) {
	app, _ := connApp(t)
	t.Setenv("VIBE_SECRET_BUCKET", "needle-bucket-R130")
	addStore(t, app, "store", "s3://needle-bucket-R130/")
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://${VIBE_SECRET_BUCKET}/c.sqlite"}})
	require.NoError(t, err)
	assert.True(t, got.Needs[0].Templated)
	assert.Nil(t, got.Needs[0].Match)
	assert.Nil(t, got.Needs[0].Suggest)
	b, _ := json.Marshal(got)
	assert.NotContains(t, string(b), "needle-bucket-R130")
	assert.NotContains(t, string(b), "store")
}

func TestFindStorageFor_Limits(t *testing.T) {
	app, _ := connApp(t)
	_, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/1", "s3://a/2", "s3://a/3", "s3://a/4", "s3://a/5"}})
	require.Error(t, err)
	_, err = app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/" + strings.Repeat("x", 2048)}})
	require.Error(t, err)
	_, err = app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/x\nforged log line"}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "forged")
	_, err = app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/1", "s3://a/2", "s3://a/3", "s3://a/4"}})
	require.NoError(t, err)
}

func TestFindStorageFor_NoWorkspaceAndNoSession(t *testing.T) {
	_, err := newBareApp(t).FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/x"}})
	require.Error(t, err)

	app, _ := connApp(t)
	addStore(t, app, "store", "s3://a/")
	app.sess = nil // the answer is definition-only; the status word is just not-connected
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/x"}})
	require.NoError(t, err)
	require.NotNil(t, got.Needs[0].Match)
	assert.Equal(t, "not-connected", got.Needs[0].Match.Status)
}

// The answer carries only what ListConnections shows: no credential, no
// status detail, whatever the storage connection holds.
func TestFindStorageFor_CarriesNoSecretOrDetail(t *testing.T) {
	app, _ := connApp(t)
	addStore(t, app, "store", "s3://a/")
	require.NoError(t, app.ws.SetCredentials(context.Background(), "store", secrets.Credential{KeyID: "AKIA" + connNeedle, Secret: connNeedle}, false))
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://a/x", "s3://b/y"}})
	require.NoError(t, err)
	assertNoSecret(t, "FindStorageFor", got)
	b, _ := json.Marshal(got)
	assert.NotContains(t, string(b), "AKIA")
	assert.NotContains(t, string(b), "detail")
}

func TestListConnectionTypes_S3CarriesPresets(t *testing.T) {
	app := newBareApp(t)
	byID := map[string]TypeSpec{}
	for _, s := range app.ListConnectionTypes() {
		byID[s.ID] = s
	}
	ids := []string{}
	for _, p := range byID["s3"].Presets {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"aws", "minio", "b2", "floci", "other"}, ids)
	assert.NotNil(t, byID["gcs"].Presets, "an empty list, never null, for the form")
	assert.Empty(t, byID["gcs"].Presets)
}

// A storage connection's own scope is expanded as at open time, so a match can
// confirm a templated scope's value (the typed URL is still never expanded).
// This pins the documented behaviour.
func TestFindStorageFor_TemplatedStorageScopeIsExpandedAsAtOpen(t *testing.T) {
	app, _ := connApp(t)
	t.Setenv("VIBE_LAKE_BUCKET", "hidden-acme")
	addStore(t, app, "tmpl", "s3://${VIBE_LAKE_BUCKET}/")
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://hidden-acme/x", "s3://guess/x"}})
	require.NoError(t, err)
	require.NotNil(t, got.Needs[0].Match)
	assert.Equal(t, "tmpl", got.Needs[0].Match.Name)
	assert.Nil(t, got.Needs[1].Match)
}

func TestFindStorageFor_ProblemIsCapitalisedAndDoesNotEchoTheURL(t *testing.T) {
	app, _ := connApp(t)
	got, err := app.FindStorageFor(FindStorageRequest{URLs: []string{"s3://other/../x", "https://h/x"}})
	require.NoError(t, err)
	for _, n := range got.Needs {
		require.NotEmpty(t, n.Problem)
		assert.Regexp(t, `^[A-Z]`, n.Problem)
		assert.NotContains(t, n.Problem, "s3://other")
	}
}
