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

func TestBucket(t *testing.T) {
	for in, want := range map[string]string{
		"s3://b/x/y.db": "s3://b/",
		"s3://b":        "s3://b/",
		"s3a://b/x":     "s3a://b/",
		"gs://b/x/":     "gs://b/",
		"abfss://c@acct.dfs.core.windows.net/p/q": "abfss://c@acct.dfs.core.windows.net/",
		"az://c/p":                            "az://c/",
		"az://acct.blob.core.windows.net/c/p": "az://acct.blob.core.windows.net/c/",
	} {
		got, err := Bucket(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"s3://", "https://b/x", "s3://b/../x", "s3://b/%2e%2e/x", "s3://b/x?y", "s3://b/*", "s3://b/[a]", "s3://b/a b",
		"abfss://c@victim.dfs.evil.example/", "az://acct.blob.core.windows.net", "local/path"} {
		_, err := Bucket(bad)
		assert.Error(t, err, bad)
	}
}

func TestBucketName(t *testing.T) {
	for in, want := range map[string]string{
		"s3://lake/x": "lake",
		"abfss://events@acct.dfs.core.windows.net/p": "events",
		"az://acct.blob.core.windows.net/data/p":     "data",
	} {
		got, err := BucketName(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestCommonFolder(t *testing.T) {
	ok := []struct {
		urls []string
		want string
	}{
		{[]string{"s3://b/catalog.sqlite"}, "s3://b/"},
		{[]string{"s3://b/wh/catalog.sqlite"}, "s3://b/wh/"},
		{[]string{"s3://b/wh/catalog.sqlite", "s3://b/wh/data/"}, "s3://b/wh/"},
		{[]string{"s3://b/wh/data/", "s3://b/wh/data/"}, "s3://b/wh/data/"},
		{[]string{"s3://b/wh/a/cat.db", "s3://b/wh/b/"}, "s3://b/wh/"},
		{[]string{"s3://b/wh/a/cat.db", "s3://b/other/"}, "s3://b/"},
		{[]string{"s3://b/tables/sales"}, "s3://b/tables/"},
		{[]string{"s3://b"}, "s3://b/"},
		{[]string{"gs://b/x/c.db"}, "gs://b/x/"},
		{[]string{"abfss://c@acct.dfs.core.windows.net/p/c.db"}, "abfss://c@acct.dfs.core.windows.net/p/"},
		// A prefix that is not a folder boundary is not a common folder.
		{[]string{"s3://b/wh/data1/f", "s3://b/wh/data2/f"}, "s3://b/wh/"},
	}
	for _, c := range ok {
		got, err := CommonFolder(c.urls)
		require.NoError(t, err, c.urls)
		assert.Equal(t, c.want, got, c.urls)
		assert.NotEqual(t, Scheme(got), got)
		_, err = Scope(got)
		assert.NoError(t, err)
		for _, u := range c.urls {
			if u+"/" == got {
				continue // a bare bucket without its slash is not within any scope (StorageFor reports it)
			}
			assert.True(t, Within(u, got), "%s within %s", u, got)
		}
	}
	for _, bad := range [][]string{
		nil,
		{"s3://a/x", "s3://ab/y"},
		{"s3://a/x", "s3a://a/x"}, // schemes are literal
		{"s3://a/x", "gs://a/x"},
		{"s3://a/x", "s3://a/../y"},
		{"s3://", "s3://a/x"},
		{"s3://a./x", "s3://a/x"},
	} {
		_, err := CommonFolder(bad)
		assert.Error(t, err, bad)
	}
}
