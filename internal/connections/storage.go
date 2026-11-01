package connections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SandwichLabs/assay/internal/secrets"
	"github.com/SandwichLabs/assay/internal/session"
	"github.com/SandwichLabs/assay/internal/storageurl"
)

// storageType is an object-store connection: s3, gcs or r2. It has no catalog:
// connecting only creates a scoped temporary secret, and the data is read with
// read_parquet('<scope>...') and friends. The scope (a URL prefix) is required
// and is the boundary the agent SQL policy (#116) allows URL reads inside.
//
//	CREATE OR REPLACE TEMPORARY SECRET "assay_x" (TYPE s3, SCOPE 's3://bucket/', KEY_ID '...',
//	    SECRET '...', REGION '...')
type storageType struct{ kind string }

func (t storageType) Name() string                      { return t.kind }
func (t storageType) Remote() bool                      { return true }
func (t storageType) Catalogless() bool                 { return true }
func (t storageType) StorageScope() bool                { return true }
func (t storageType) Extensions() []string              { return []string{"httpfs"} }
func (t storageType) NeedsCredential(session.Spec) bool { return true }

var urlStyles = []string{"vhost", "path"}

func (t storageType) schemes() []string {
	switch t.kind {
	case "gcs":
		return []string{"gcs://", "gs://"}
	case "r2":
		return []string{"r2://"}
	}
	return []string{"s3://", "s3a://", "s3n://"}
}

func (t storageType) allowedOptions() []string {
	switch t.kind {
	case "r2":
		return []string{"account_id"}
	case "gcs":
		return nil
	}
	return []string{"endpoint", "url_style", "use_ssl"}
}

func (t storageType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "scope", "region"); err != nil {
		return err
	}
	scope := param(s, "scope")
	if err := requireField(s, "a scope (a URL prefix such as "+t.schemes()[0]+"bucket/path/)", scope); err != nil {
		return err
	}
	ok := false
	for _, p := range t.schemes() {
		if strings.HasPrefix(scope, p) && len(scope) > len(p) {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%s connection %q: scope %q must start with %s and name a bucket", t.kind, s.Name, scope, strings.Join(t.schemes(), " or "))
	}
	if err := storageurl.Validate(scope); err != nil {
		return fmt.Errorf("%s connection %q: scope must be a plain URL prefix: %w", t.kind, s.Name, err)
	}
	if r := param(s, "region"); r != "" && !regionRE.MatchString(r) {
		return fmt.Errorf("%s connection %q: region %q is not a region name", t.kind, s.Name, r)
	}
	if err := unknownOptions(s, t.allowedOptions()...); err != nil {
		return err
	}
	if ep := opt(s, "endpoint"); ep != "" && !endptRE.MatchString(ep) {
		return fmt.Errorf("%s connection %q: endpoint %q is not a host name (no scheme, no path)", t.kind, s.Name, ep)
	}
	if err := oneOf("url_style", opt(s, "url_style"), urlStyles...); err != nil {
		return err
	}
	if err := oneOf("use_ssl", opt(s, "use_ssl"), "true", "false"); err != nil {
		return err
	}
	if t.kind == "r2" {
		a := opt(s, "account_id")
		if a == "" {
			return fmt.Errorf("r2 connection %q needs the account id (option account_id)", s.Name)
		}
		if !acctRE.MatchString(a) {
			return fmt.Errorf("r2 connection %q: account_id %q is not an account id", s.Name, a)
		}
	}
	return nil
}

func (t storageType) SecretSteps(s session.Spec, c secrets.Credential) ([]session.SecretStep, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if c.KeyID == "" || c.Secret == "" {
		return nil, fmt.Errorf("%s connection %q needs an access key id and secret", t.kind, s.Name)
	}
	params := []session.SecretParam{
		{Key: "SCOPE", Value: param(s, "scope")},
		{Key: "KEY_ID", Value: c.KeyID},
		{Key: "SECRET", Value: c.Secret, Secret: true},
	}
	// Every field is explicit. DuckDB fills a field a secret leaves out from its
	// s3_* settings, and httpfs fills those from the host's AWS_* environment at
	// load (#135), so an omitted SESSION_TOKEN or REGION would be the host's.
	token := ""
	if t.kind == "s3" {
		token = c.Token
	}
	params = append(params,
		session.SecretParam{Key: "SESSION_TOKEN", Value: token, Secret: true},
		session.SecretParam{Key: "REGION", Value: storageRegion(t.kind, param(s, "region"))})
	switch t.kind {
	case "s3":
		if e := opt(s, "endpoint"); e != "" {
			params = append(params, session.SecretParam{Key: "ENDPOINT", Value: e})
		}
		if u := opt(s, "url_style"); u != "" {
			params = append(params, session.SecretParam{Key: "URL_STYLE", Value: u})
		}
		if u := opt(s, "use_ssl"); u != "" {
			params = append(params, session.SecretParam{Key: "USE_SSL", Value: u == "true"})
		}
	case "r2":
		params = append(params, session.SecretParam{Key: "ACCOUNT_ID", Value: opt(s, "account_id")})
	}
	return []session.SecretStep{{Name: secretName(s.Name), Type: t.kind, Params: params}}, nil
}

// AttachSQL: a storage connection attaches nothing.
func (t storageType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	return []string{}, nil
}

// Probe lists up to 1000 objects under the scope: one listing round trip, which
// proves the key, the region and the scope.
func (t storageType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	return t.probeListing(ctx, sess, s)
}

// probeListing lists the scope in a private session (session.ObjectLister):
// the hub reads S3-like URLs literally, so it cannot glob (#138).
func (t storageType) probeListing(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	lister, ok := sess.(session.ObjectLister)
	if !ok {
		return 0, "", errors.New("this session cannot list a scope")
	}
	if s.Credential == nil {
		return 0, "", fmt.Errorf("%s connection %q has no credential to list with", t.kind, s.Name)
	}
	cred, err := s.Credential(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("could not resolve the credential to list with: %w", err)
	}
	steps, err := t.SecretSteps(s, cred)
	if err != nil {
		return 0, "", err
	}
	if len(steps) != 1 {
		return 0, "", fmt.Errorf("%s connection %q has no single secret to list with", t.kind, s.Name)
	}
	scope, err := storageurl.Scope(param(s, "scope"))
	if err != nil {
		return 0, "", err
	}
	n, err := lister.ListObjects(ctx, steps[0], scope+"**", maxListed)
	if err != nil {
		return 0, "", fmt.Errorf("could not list the scope: %w", err)
	}
	return listingResult(n)
}

// maxListed bounds the listing a Test makes.
const maxListed = 1000

// listingResult formats a listing's object count.
func listingResult(n int) (int, string, error) {
	if n == maxListed {
		return n, "1000+ objects", nil
	}
	return n, fmt.Sprintf("%d objects", n), nil
}

// probeScope is the Azure connections' Test: one listing of the scope. The
// pattern is "**" so objects in sub-folders count too (a lone "*" stops at the
// first folder level and reported "0 objects" for a scope of folders). Azure
// does not read through httpfs, so the hub's S3 URL rules do not touch it.
func probeScope(ctx context.Context, sess session.Session, scope string) (int, string, error) {
	var n int
	err := sess.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM (SELECT file FROM glob(`+session.QuoteLiteral(scope+"**")+`) LIMIT `+fmt.Sprint(maxListed)+`)`).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("could not list the scope: %w", err)
	}
	return listingResult(n)
}

func (t storageType) Info() TypeInfo {
	label := map[string]string{"s3": "Amazon S3 (or S3-compatible)", "gcs": "Google Cloud Storage", "r2": "Cloudflare R2"}[t.kind]
	fields := []Field{
		{Name: "scope", Label: "Scope", Kind: KindURL, Required: true, Help: "A URL prefix such as " + t.schemes()[0] + "bucket/path/. Reads are allowed only inside it."},
	}
	fp := []string{"type", "scope"}
	switch t.kind {
	case "s3":
		fields = append(fields,
			Field{Name: "region", Label: "Region", Kind: KindText, Default: "us-east-1"},
			Field{Name: "endpoint", Label: "Endpoint", Kind: KindText, Option: true, Help: "For S3-compatible stores (MinIO and others)."},
			Field{Name: "url_style", Label: "URL style", Kind: KindEnum, Choices: urlStyles, Option: true},
			Field{Name: "use_ssl", Label: "Use TLS", Kind: KindEnum, Choices: []string{"true", "false"}, Default: "true", Option: true, Help: "Turn off only for a local S3-compatible store; traffic and keys are sent unencrypted."},
		)
		fp = append(fp, "region", "option:endpoint", "option:url_style", "option:use_ssl")
	case "r2":
		fields = append(fields, Field{Name: "account_id", Label: "Account id", Kind: KindText, Required: true, Option: true})
		fp = append(fp, "option:account_id")
	}
	creds := []Field{
		{Name: "key_id", Label: "Access key id", Kind: KindText, Required: true, PerPerson: true},
		{Name: "secret", Label: "Secret access key", Kind: KindText, Required: true, Secret: true, PerPerson: true},
	}
	if t.kind == "s3" {
		creds = append(creds, Field{Name: "token", Label: "Session token", Kind: KindText, Secret: true, PerPerson: true})
	}
	return TypeInfo{
		Name: t.kind, Label: label, Fields: fields, Credentials: creds,
		Extensions:        []string{"httpfs"},
		Capabilities:      Capabilities{Catalog: false, Writable: false, ReadOnlyAttach: false, Remote: true, Test: true},
		FingerprintFields: fp,
		SignInMethods:     storageSignIns(t.kind),
		Presets:           storagePresets(t.kind),
	}
}

// storageSignIns are the sign-in methods of a storage type. Only s3 takes an
// AWS SSO profile; the others have no AWS identity to sign in with.
func storageSignIns(kind string) []string {
	if kind == "s3" {
		return []string{"keychain", "env", "prompt", "aws-sso"}
	}
	return []string{"keychain", "env", "prompt"}
}

// StorageTypeForScheme returns the storage connection type whose scope can
// hold URLs of the scheme ("s3://" -> "s3"), and false for https:// and any
// scheme that is not object storage. It reads the types' own scheme lists, so
// a new scheme cannot drift from them. Schemes are literal: gs:// and gcs://
// both mean gcs, but a gs:// URL still never matches a gcs:// scope.
func StorageTypeForScheme(scheme string) (string, bool) {
	if !strings.Contains(scheme, "://") {
		scheme += "://"
	}
	for _, kind := range []string{"s3", "gcs", "r2"} {
		if slices.Contains((storageType{kind: kind}).schemes(), scheme) {
			return kind, true
		}
	}
	if slices.Contains(storageurl.AzureSchemes, scheme) {
		return "azure", true
	}
	return "", false
}

// storageRegion is the region a secret is written with: the connection's own,
// else the provider's default (never the host's AWS_REGION, #135).
func storageRegion(kind, region string) string {
	switch {
	case region != "":
		return region
	case kind == "s3":
		return "us-east-1"
	default:
		return "auto"
	}
}
