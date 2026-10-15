package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addStore(t *testing.T, ws Workspace, name, typ, scope string, in Scope) {
	t.Helper()
	require.NoError(t, ws.AddConnection(in, ConnectionDef{Name: name, Type: typ, Scope: scope}))
}

// storageWS has a spread of storage connections: nested scopes, a tie, two
// schemes for one cloud and a user-scope one.
func storageWS(t *testing.T) Workspace {
	ws, _, _ := memWorkspace(t, nil)
	addStore(t, ws, "wide", "s3", "s3://b/", ScopeLocal)
	addStore(t, ws, "narrow", "s3", "s3://b/wh/", ScopeLocal)
	addStore(t, ws, "tie_b", "s3", "s3://t/", ScopeLocal)
	addStore(t, ws, "tie_a", "s3", "s3://t/", ScopeUser)
	addStore(t, ws, "gcs_one", "gcs", "gcs://g/", ScopeLocal)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "az1", Type: "azure", Scope: "az://acct.blob.core.windows.net/",
		Options: map[string]string{"account_name": "acct"}}))
	return ws
}

// TestStorageForMatchesTheOpenTimeCheck feeds one table of URLs to both rules:
// StorageFor and checkStorageURL must agree on every one.
func TestStorageForMatchesTheOpenTimeCheck(t *testing.T) {
	ws := storageWS(t)
	for _, u := range []string{
		"s3://b/x.db", "s3://b/wh/x.db", "s3://b/wh/", "s3://b/whx/y", "s3://t/c.db",
		"gcs://g/c.db", "gs://g/c.db", "s3a://b/x.db", "s3://other/x.db", "s3://bb/x",
		"az://acct.blob.core.windows.net/c", "az://acct.blob.core.windows.net/c/x.db", "az://acct.blob.core.windows.net",
	} {
		name, err := checkStorageURL(ws, "lake", u, "catalog", nil)
		need := StorageFor(ws, []string{u}, nil)[0]
		if err != nil {
			assert.Nil(t, need.Match, u)
			assert.True(t, need.Suggest != nil || need.Problem != "", u)
			continue
		}
		require.NotNil(t, need.Match, u)
		assert.Equal(t, name, need.Match.Name, u)
	}
	n := StorageFor(ws, []string{"s3://b/wh/c.db", "s3://t/c.db"}, nil)
	assert.Equal(t, "narrow", n[0].Match.Name, "longest scope wins")
	assert.Equal(t, ScopeLocal, n[0].Match.In)
	assert.Equal(t, "tie_a", n[1].Match.Name, "a tie goes to the first by name")
	assert.Equal(t, ScopeUser, n[1].Match.In)
}

func TestStorageForSchemesAreLiteral(t *testing.T) {
	ws := storageWS(t)
	n := StorageFor(ws, []string{"gs://g/c.db"}, nil)[0]
	assert.Nil(t, n.Match, "a gs:// URL never matches a gcs:// scope")
	require.NotNil(t, n.Suggest)
	assert.Equal(t, "gcs", n.Suggest.Type)
	assert.Equal(t, "gs://g/", n.Suggest.Scope, "the suggestion keeps the scheme as typed")
	assert.Equal(t, "g_gcs", n.Suggest.Name)
}

func TestStorageForSuggestions(t *testing.T) {
	ws := storageWS(t)

	// Catalog and data path in one folder: one suggestion, that folder.
	n := StorageFor(ws, []string{"s3://lake/wh/catalog.sqlite", "s3://lake/wh/data/"}, nil)
	require.NotNil(t, n[0].Suggest)
	assert.Same(t, n[0].Suggest, n[1].Suggest)
	assert.Equal(t, "s3://lake/wh/", n[0].Suggest.Scope)
	assert.Equal(t, "s3://lake/", n[0].Suggest.BucketScope)
	assert.Equal(t, "s3", n[0].Suggest.Type)
	assert.Equal(t, "lake_s3", n[0].Suggest.Name)

	// Never shorter than the bucket, never the bare scheme.
	n = StorageFor(ws, []string{"s3://lake/catalog.sqlite"}, nil)
	assert.Equal(t, "s3://lake/", n[0].Suggest.Scope)

	// Two buckets: two suggestions with different names.
	n = StorageFor(ws, []string{"s3://a/x", "s3://ab/y"}, nil)
	require.NotNil(t, n[0].Suggest)
	require.NotNil(t, n[1].Suggest)
	assert.Equal(t, "s3://a/", n[0].Suggest.Scope)
	assert.Equal(t, "s3://ab/", n[1].Suggest.Scope)
	assert.NotEqual(t, n[0].Suggest.Name, n[1].Suggest.Name)

	// Mixed schemes for one bucket name: two suggestions.
	n = StorageFor(ws, []string{"s3://a/x", "s3a://a/y"}, nil)
	assert.Equal(t, "s3://a/", n[0].Suggest.Scope)
	assert.Equal(t, "s3a://a/", n[1].Suggest.Scope)
	assert.NotEqual(t, n[0].Suggest.Name, n[1].Suggest.Name)

	// An abfss account host is kept.
	n = StorageFor(ws, []string{"abfss://events@acct.dfs.core.windows.net/p/c.db"}, nil)
	require.NotNil(t, n[0].Suggest)
	assert.Equal(t, "azure", n[0].Suggest.Type)
	assert.Equal(t, "abfss://events@acct.dfs.core.windows.net/p/", n[0].Suggest.Scope)
	assert.Equal(t, "events_azure", n[0].Suggest.Name)
}

func TestStorageForProblems(t *testing.T) {
	ws := storageWS(t)
	for _, u := range []string{
		"s3://lake/../x", "s3://lake/%2e%2e/x", "s3://lake/x?y=1", "s3://lake/*", "s3://lake/[a]",
		"s3://lake/a b", "s3://lake/a//b", "s3://", "s3://lake", "local/path.db",
		"abfss://c@victim.dfs.evil.example/x",
	} {
		n := StorageFor(ws, []string{u}, nil)[0]
		assert.NotEmpty(t, n.Problem, u)
		assert.Nil(t, n.Suggest, u)
		assert.Nil(t, n.Match, u)
	}
	n := StorageFor(ws, []string{"https://host/b/x.parquet"}, nil)[0]
	assert.Contains(t, n.Problem, "S3 connection with an endpoint")
	assert.Nil(t, n.Suggest)
}

func TestStorageForNamesAvoidExistingAndReserved(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	addStore(t, ws, "lake_s3", "s3", "s3://other/", ScopeUser) // different scope, same name wanted
	assert.Equal(t, "lake_s3_2", StorageFor(ws, []string{"s3://lake/x"}, nil)[0].Suggest.Name)
	assert.Equal(t, "memory_s3", StorageFor(ws, []string{"s3://memory/x"}, nil)[0].Suggest.Name)
	assert.Equal(t, "s3_2024_data", StorageFor(ws, []string{"s3://2024-data/x"}, nil)[0].Suggest.Name, "a leading digit puts the type first")
	assert.Equal(t, "my_bucket_s3", StorageFor(ws, []string{"s3://My.Bucket/x"}, nil)[0].Suggest.Name)
	long := "s3://" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "/x"
	assert.NoError(t, ValidateConnectionName(StorageFor(ws, []string{long}, nil)[0].Suggest.Name))
}

type fakeConnsWS struct {
	Workspace
	conns []Connection
}

func (f fakeConnsWS) Connections() []Connection { return f.conns }

// A connection of a non-storage type is ignored however its scope looks.
func TestStorageForIgnoresNonStorageTypes(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	fake := fakeConnsWS{Workspace: ws, conns: []Connection{
		{ConnectionDef: ConnectionDef{Name: "pg", Type: "postgres", Scope: "s3://b/"}, Scope: ScopeLocal},
	}}
	n := StorageFor(fake, []string{"s3://b/x"}, nil)[0]
	assert.Nil(t, n.Match)
	assert.NotNil(t, n.Suggest)
}

func TestStorageForConnectionNeverCarriesAnExpandedValue(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	lookup := func(k string) (string, bool) {
		if k == "LAKE_BUCKET" {
			return "hunter2bucket", true
		}
		return "", false
	}
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "lake", Type: "ducklake",
		Path: "s3://${LAKE_BUCKET}/wh/cat.db", Options: map[string]string{"catalog": "sqlite", "data_path": "s3://plain/wh/data/"}}))
	c, ok := ws.Connection("lake")
	require.True(t, ok)
	needs := StorageForConnection(ws, c, lookup)
	require.Len(t, needs, 2)
	assert.Equal(t, "catalog", needs[0].What)
	assert.True(t, needs[0].Templated)
	assert.Equal(t, "s3://${LAKE_BUCKET}/wh/cat.db", needs[0].URL)
	require.NotNil(t, needs[0].Suggest)
	assert.Empty(t, needs[0].Suggest.Scope)
	assert.Empty(t, needs[0].Suggest.BucketScope)
	assert.NotContains(t, needs[0].Suggest.Name, "hunter2")
	assert.Equal(t, "data path", needs[1].What)
	assert.False(t, needs[1].Templated)
	assert.Equal(t, "s3://plain/wh/data/", needs[1].Suggest.Scope)

	// An unset variable gives a hint on the typed value, not an error.
	needs = StorageForConnection(ws, c, func(string) (string, bool) { return "", false })
	require.NotEmpty(t, needs)
	for _, n := range needs {
		if n.Templated {
			assert.Equal(t, "s3://${LAKE_BUCKET}/wh/cat.db", n.URL)
		}
	}
}

func TestStorageForConnectionTypedScopeAndGroups(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	lookup := func(k string) (string, bool) {
		switch k {
		case "SB":
			return "secretbucket", true
		case "A":
			return "bucketa", true
		case "B":
			return "bucketb", true
		case "X":
			return "plain", true
		}
		return "", false
	}
	// A matching storage connection with a templated scope: the match is made
	// on the expanded scope, but reported as typed.
	addStore(t, ws, "store", "s3", "s3://${SB}/", ScopeLocal)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "lake", Type: "ducklake",
		Path: "s3://${SB}/c.db", Options: map[string]string{"catalog": "sqlite", "data_path": "s3://${SB}/d/"}}))
	c, _ := ws.Connection("lake")
	for _, n := range StorageForConnection(ws, c, lookup) {
		require.NotNil(t, n.Match)
		assert.Equal(t, "store", n.Match.Name)
		assert.Equal(t, "s3://${SB}/", n.Match.Scope)
	}
	// ... also for a plain lake URL.
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "plain_lake", Type: "iceberg", Path: "s3://secretbucket/t/"}))
	c, _ = ws.Connection("plain_lake")
	n := StorageForConnection(ws, c, lookup)[0]
	assert.Equal(t, "s3://${SB}/", n.Match.Scope)

	// Two templated buckets: two groups, two names, nothing expanded.
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "two", Type: "ducklake",
		Path: "s3://${A}/c.db", Options: map[string]string{"catalog": "sqlite", "data_path": "s3://${B}/d/"}}))
	c, _ = ws.Connection("two")
	needs := StorageForConnection(ws, c, lookup)
	require.Len(t, needs, 2)
	assert.NotEqual(t, needs[0].Group, needs[1].Group)
	assert.NotEqual(t, needs[0].Suggest.Name, needs[1].Suggest.Name)
	for _, n := range needs {
		assert.Empty(t, n.Suggest.Scope)
		assert.NotContains(t, n.Suggest.Name, "bucket")
	}

	// A templated URL and a plain URL in one bucket: one group, nothing shown.
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "mixed", Type: "ducklake",
		Path: "s3://${X}/c.db", Options: map[string]string{"catalog": "sqlite", "data_path": "s3://plain/d/"}}))
	c, _ = ws.Connection("mixed")
	needs = StorageForConnection(ws, c, lookup)
	require.Len(t, needs, 2)
	assert.Equal(t, needs[0].Group, needs[1].Group)
	assert.Equal(t, needs[0].Suggest.Name, needs[1].Suggest.Name)
	assert.Empty(t, needs[1].Suggest.Scope)
}
