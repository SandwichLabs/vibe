package storageurl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeIsAPlainStorageFolder(t *testing.T) {
	for in, want := range map[string]string{
		"s3://bucket":                      "s3://bucket/",
		"s3://bucket/p":                    "s3://bucket/p/",
		"s3://bucket/p/":                   "s3://bucket/p/",
		"az://c/x":                         "az://c/x/",
		"abfss://c@a.dfs.core.windows.net": "abfss://c@a.dfs.core.windows.net/",
		"gs://b/":                          "gs://b/",
		"  r2://b  ":                       "r2://b/",
	} {
		got, err := Scope(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{
		"", "s3://", "file:///", "file:///etc", "http://x/", "https://x/", "hf://datasets/x", "ftp://x", "S3://b/", "s3:/b/", "b/p",
		"s3://b/a b", "s3://b/*", "s3://b/p?x=1", "s3://b/p#f", "s3://b/a\\b", "s3://b/%2e%2e/c", "s3://b/%2E%2E", "s3://b/a%2fb",
		"s3://b/../c", "s3://b/./c", "s3://b//c", "s3:///b", "s3://b/a\x00", "s3://b/a\nb",
	} {
		_, err := Scope(in)
		assert.Error(t, err, "%q", in)
	}
}

func TestWithinIsOneLiteralPrefixRule(t *testing.T) {
	scope := "s3://lake/raw"
	for _, u := range []string{"s3://lake/raw/a.parquet", "s3://lake/raw/2024/*.parquet", "s3://lake/raw/x/y/z"} {
		assert.True(t, Within(u, scope), u)
	}
	for _, u := range []string{
		"s3://lake/raw", "s3://lake/rawx/a", "s3://lake-evil/raw/a", "s3://lake/other/a", "s3://lake/raw/../other/a", "s3://lake/raw/%2e%2e/other/a",
		"s3://lake/raw//a", "s3://lake/raw/./a", "s3://lake/raw/a?x=1", "s3://lake/raw/a#f", "S3://lake/raw/a", "s3a://lake/raw/a", "s3n://lake/raw/a",
		"gs://lake/raw/a", "https://lake.s3.amazonaws.com/raw/a", "file:///lake/raw/a", "s3://LAKE/raw/a", "s3://lake/Raw/a", "s3://lake/raw/a b",
	} {
		assert.False(t, Within(u, scope), u)
	}
	// Aliases are never guessed at, in either direction.
	assert.False(t, Within("gs://b/x", "gcs://b/"))
	assert.False(t, Within("gcs://b/x", "gs://b/"))
	assert.False(t, Within("azure://c/x", "az://c/"))
	assert.False(t, Within("abfss://c@a.dfs.core.windows.net/x", "az://c/"))
	// A bad scope contains nothing.
	assert.False(t, Within("file:///etc/hostname", "file:///"))
	assert.False(t, Within("http://127.0.0.1/x", "http://"))
	assert.False(t, Within("s3://b/x", "s3://b/*"))
	// A read may use a glob; a scope may not.
	assert.NoError(t, ValidateRead("s3://b/a/*.parquet"))
	assert.Error(t, Validate("s3://b/a/*.parquet"))
}

func TestAzureAccountMustBeTheAccountsOwnEndpoint(t *testing.T) {
	for u, want := range map[string]string{
		"az://container/path/":                       "",
		"azure://container":                          "",
		"az://acmelake.blob.core.windows.net/c/p":    "acmelake",
		"abfss://c@acmelake.dfs.core.windows.net/p/": "acmelake",
		"abfss://c@acmelake.dfs.core.windows.net":    "acmelake",
	} {
		got, err := AzureAccount(u)
		require.NoError(t, err, u)
		assert.Equal(t, want, got, u)
	}
	for _, u := range []string{
		"abfss://c@victim.dfs.evil.com/", "abfss://c@acmelake.dfs.core.windows.net.evil.com/", "abfss://c@evil.com/acmelake.dfs.core.windows.net/",
		"abfss://c@acmelake.blob.core.windows.net/", "abfss://acmelake.dfs.core.windows.net/c/", "abfss://@acmelake.dfs.core.windows.net/", "abfss://c@@acmelake.dfs.core.windows.net/",
		"az://acmelake.dfs.core.windows.net/c/", "az://acmelake.blob.core.windows.net.evil.com/c/", "az://evil.com/c/", "az://127.0.0.1:10000/devstoreaccount1/c/",
		"az://c@acmelake.blob.core.windows.net/c/", "az://AcmeLake.blob.core.windows.net/c/", "abfss://c@ab.dfs.core.windows.net/", "s3://b/x",
		"az://c/%2e%2e/x", "abfss://c@acmelake.dfs.core.windows.net/../x",
	} {
		_, err := AzureAccount(u)
		assert.Error(t, err, u)
	}
	assert.True(t, ValidAzureAccount("acmelake"))
	assert.False(t, ValidAzureAccount("acme;AccountKey=x"))
}

func TestGlobCharactersAreRefused(t *testing.T) {
	for _, u := range []string{"s3://b/a[1].db", "s3://b/a[12].db", "s3://b/a{1,2}.db", "s3://b/a!.db", "s3://b/a]", "s3://b/{x}/c", "s3://b/a?.db"} {
		assert.Error(t, Validate(u), u)
		assert.Error(t, ValidateRead(u), u)
		assert.False(t, Within(u, "s3://b/"), u)
	}
	// A read may still carry a "*"; a plain URL may not.
	assert.NoError(t, ValidateRead("s3://b/a/*.parquet"))
	assert.Error(t, Validate("s3://b/a/*.parquet"))
}
