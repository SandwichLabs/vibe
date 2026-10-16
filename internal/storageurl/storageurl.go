// Package storageurl is the one place that decides what an object-storage URL
// is: which schemes count, what a plain URL looks like, whether one lies inside
// a scope, and whether an Azure URL names the account's own endpoint. The agent
// SQL policy (what a URL read may reach), the workspace (where a lake or table
// may live) and the connection types (what a scope may be) all use it, so they
// cannot disagree (#119 review).
//
// A URL is accepted only as a plain literal: a known storage scheme written in
// lower case, no query string or fragment, no percent-escapes (so "%2e%2e" is
// no way round a ".." check), no backslash, space or control character, no "."
// or ".." segment, and no empty path segment ("//"). Scheme aliases are never
// guessed at: "gs://" does not match a "gcs://" scope.
package storageurl

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Schemes are the URL schemes an object-storage connection may use.
var Schemes = []string{"s3://", "s3a://", "s3n://", "gcs://", "gs://", "r2://", "az://", "azure://", "abfss://"}

// AzureSchemes are the schemes of an Azure scope.
var AzureSchemes = []string{"az://", "azure://", "abfss://"}

// Scheme returns the storage scheme prefix of u ("s3://"), or "".
func Scheme(u string) string {
	for _, s := range Schemes {
		if strings.HasPrefix(u, s) {
			return s
		}
	}
	return ""
}

// Validate returns an error unless u is a plain storage URL (see the package
// comment) with something after the scheme.
func Validate(u string) error { return validate(u, false) }

// ValidateRead is Validate for a URL a read names. A "*" wildcard is allowed in
// an Azure URL (a glob such as az://c/raw/2024/*.parquet), and in no other: the
// session reads an S3-like URL literally, "?" and "*" included, so that no URL
// can carry a different endpoint for the credential to be sent to (#138).
func ValidateRead(u string) error { return validate(u, true) }

func validate(u string, read bool) error {
	sch := Scheme(u)
	glob := read && slices.Contains(AzureSchemes, sch)
	if sch == "" {
		return fmt.Errorf("%q does not start with one of %s", u, strings.Join(Schemes, ", "))
	}
	rest := u[len(sch):]
	if rest == "" {
		return fmt.Errorf("%q names no bucket or container", u)
	}
	for _, r := range u {
		switch {
		case r < 0x20 || r == 0x7f, r == ' ', r == '\\', r == '?', r == '#', r == '%', r == '*' && !glob:
			return fmt.Errorf("%q must be a plain URL (no spaces, wildcards, query string, fragment or escapes)", u)
		case r == '[', r == ']', r == '{', r == '}', r == '!':
			// DuckDB reads these as glob syntax (a[1].db is a1.db), so they would name
			// a different object than the one that was checked.
			return fmt.Errorf("%q must be a plain URL (no glob characters such as [ ] { } !)", u)
		}
	}
	segs := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			if i == 0 && seg == "" {
				return fmt.Errorf("%q names no bucket or container", u)
			}
			return fmt.Errorf("%q has an empty, \".\" or \"..\" path segment", u)
		}
	}
	return nil
}

// Scope validates a connection's scope and returns it as a folder prefix (with
// a trailing slash), the form reads are matched against.
func Scope(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := Validate(s); err != nil {
		return "", err
	}
	if !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s, nil
}

// Within reports whether the plain URL u (an Azure read may carry a "*") lies inside the scope: a literal,
// case-sensitive prefix match against the scope's folder form.
func Within(u, scope string) bool {
	sc, err := Scope(scope)
	if err != nil || ValidateRead(u) != nil {
		return false
	}
	return strings.HasPrefix(u, sc)
}

var (
	azAccountRE = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	blobHostRE  = regexp.MustCompile(`^([a-z0-9]{3,24})\.blob\.core\.windows\.net$`)
	dfsHostRE   = regexp.MustCompile(`^([a-z0-9]{3,24})\.dfs\.core\.windows\.net$`)
)

// AzureAccount checks that an Azure URL (az://, azure:// or abfss://) can only
// send the account's credential to the account's own endpoint, and returns the
// account the URL names ("" for the short az://container/ form, which uses the
// secret's account).
//
//   - abfss://container@<account>.dfs.core.windows.net/path
//   - az://container/path and azure://container/path (no account in the URL)
//   - az://<account>.blob.core.windows.net/container/path (fully qualified)
//
// Any other host (abfss://c@victim.dfs.evil.example/) is refused: a secret whose
// scope named it would send its Authorization header there.
func AzureAccount(u string) (string, error) {
	if err := Validate(u); err != nil {
		return "", err
	}
	sch := Scheme(u)
	rest := strings.TrimSuffix(u[len(sch):], "/")
	first := strings.SplitN(rest, "/", 2)[0]
	switch sch {
	case "abfss://":
		cont, host, ok := strings.Cut(first, "@")
		if !ok || cont == "" || strings.Contains(host, "@") {
			return "", fmt.Errorf("%q must look like abfss://container@account.dfs.core.windows.net/path", u)
		}
		m := dfsHostRE.FindStringSubmatch(host)
		if m == nil {
			return "", fmt.Errorf("%q: the host must be <account>.dfs.core.windows.net", u)
		}
		return m[1], nil
	case "az://", "azure://":
		if strings.Contains(first, "@") {
			return "", fmt.Errorf("%q: an @ is only used by abfss:// URLs", u)
		}
		if strings.Contains(first, ".") {
			m := blobHostRE.FindStringSubmatch(first)
			if m == nil {
				return "", fmt.Errorf("%q: a fully qualified host must be <account>.blob.core.windows.net", u)
			}
			return m[1], nil
		}
		return "", nil
	}
	return "", fmt.Errorf("%q is not an Azure URL", u)
}

// Bucket returns the bucket (or container) a plain storage URL lies in, as a
// folder URL with its own scheme: "s3://b/" for s3://b/x/y.db. For an
// abfss:// URL it keeps the container and the account host
// (abfss://c@acct.dfs.core.windows.net/), and for a fully qualified az:// URL
// the host and container. An Azure URL whose host is not the account's own
// endpoint is refused, so a suggestion built from it could not send a
// credential elsewhere.
func Bucket(u string) (string, error) {
	if err := Validate(u); err != nil {
		return "", err
	}
	sch := Scheme(u)
	if sch == "az://" || sch == "azure://" || sch == "abfss://" {
		if _, err := AzureAccount(u); err != nil {
			return "", err
		}
	}
	segs := strings.Split(strings.TrimSuffix(u[len(sch):], "/"), "/")
	n := 1
	if (sch == "az://" || sch == "azure://") && strings.Contains(segs[0], ".") {
		n = 2 // az://<account>.blob.core.windows.net/<container>/
	}
	if len(segs) < n {
		return "", fmt.Errorf("%q names no container", u)
	}
	return sch + strings.Join(segs[:n], "/") + "/", nil
}

// BucketName is the short, human name of a URL's bucket or container ("lake"
// for s3://lake/x, "events" for abfss://events@acct.dfs.core.windows.net/).
func BucketName(u string) (string, error) {
	b, err := Bucket(u)
	if err != nil {
		return "", err
	}
	segs := strings.Split(strings.TrimSuffix(b[len(Scheme(b)):], "/"), "/")
	name := segs[len(segs)-1]
	name, _, _ = strings.Cut(name, "@")
	return name, nil
}

// CommonFolder returns the deepest folder (with a trailing slash) that holds
// every URL: the catalog's folder rather than the catalog file. A URL ending in
// "/" is a folder itself; any other is an object (or a table folder written
// without the slash) and counts through its parent, which is the only scope
// that Within would match it against. The URLs must share one scheme and one
// bucket; the result is never shorter than the bucket and never the bare
// scheme, and always passes Scope.
func CommonFolder(urls []string) (string, error) {
	if len(urls) == 0 {
		return "", fmt.Errorf("no URL to find a folder for")
	}
	bucket, err := Bucket(urls[0])
	if err != nil {
		return "", err
	}
	var common []string
	for i, u := range urls {
		b, err := Bucket(u)
		if err != nil {
			return "", err
		}
		if b != bucket {
			return "", fmt.Errorf("%q and %q are not in the same bucket", urls[0], u)
		}
		segs := folderSegments(u, bucket)
		if i == 0 {
			common = segs
			continue
		}
		k := 0
		for k < len(common) && k < len(segs) && common[k] == segs[k] {
			k++
		}
		common = common[:k]
	}
	out := bucket
	if len(common) > 0 {
		out += strings.Join(common, "/") + "/"
	}
	return Scope(out)
}

// folderSegments are the folder segments of u below its bucket (the file name,
// when u does not end in "/", is dropped).
func folderSegments(u, bucket string) []string {
	rest := strings.TrimPrefix(u, bucket)
	if rest == "" || u+"/" == bucket {
		return nil
	}
	segs := strings.Split(rest, "/")
	return segs[:len(segs)-1] // the last element is "" after a trailing slash, else the object
}
