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

// ValidateRead is Validate for a URL a read names: a "*" wildcard is allowed in
// the last segments (a glob such as s3://b/raw/2024/*.parquet).
func ValidateRead(u string) error { return validate(u, true) }

func validate(u string, glob bool) error {
	sch := Scheme(u)
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

// Within reports whether the plain URL u (a read may carry a "*") lies inside the scope: a literal,
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
