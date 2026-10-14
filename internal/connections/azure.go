package connections

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/storageurl"
)

// azureType is Azure Blob / Data Lake storage. Like s3 it attaches no catalog:
// connecting only creates a scoped temporary secret, the data is read with
// read_parquet('<scope>...') and friends, and the scope (a URL prefix) is the
// boundary the agent SQL policy allows URL reads inside.
//
// Three ways to sign in (option "sign_in"); credential_chain is not offered
// because it would pick up whatever Azure credentials the process happens to
// have (az login, environment, managed identity) instead of the ones the
// person entered:
//
//	connection_string  CREATE TEMPORARY SECRET (TYPE azure, CONNECTION_STRING '...', SCOPE '...')
//	account_key        the same, with the string built from the account name and key
//	service_principal  CREATE TEMPORARY SECRET (TYPE azure, PROVIDER service_principal,
//	                   TENANT_ID '...', CLIENT_ID '...', CLIENT_SECRET '...', ACCOUNT_NAME '...', SCOPE '...')
//
// SAS tokens are not offered either: in a URL they would sit in the scope, and
// the scope is shown, logged and shared.
type azureType struct{}

func (azureType) Name() string                      { return "azure" }
func (azureType) Remote() bool                      { return true }
func (azureType) Catalogless() bool                 { return true }
func (azureType) StorageScope() bool                { return true }
func (azureType) Extensions() []string              { return []string{"azure"} }
func (azureType) NeedsCredential(session.Spec) bool { return true }

// AzureSchemes are the URL schemes an azure scope may use.
var AzureSchemes = storageurl.AzureSchemes

const (
	azureAuthConnectionString  = "connection_string"
	azureAuthAccountKey        = "account_key"
	azureAuthServicePrincipal  = "service_principal"
	azureDefaultAuth           = azureAuthAccountKey
	azureEndpointSuffixDefault = "core.windows.net"
)

var (
	azureAuths    = []string{azureAuthConnectionString, azureAuthAccountKey, azureAuthServicePrincipal}
	azureAccountR = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	azureTenantR  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-]{0,127}$`)
	azureClientR  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-]{0,127}$`)
	azureKeyR     = regexp.MustCompile(`^[A-Za-z0-9+/]{8,}={0,2}$`)
)

func azureAuth(s session.Spec) string {
	if a := opt(s, "sign_in"); a != "" {
		return a
	}
	return azureDefaultAuth
}

func (azureType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "scope"); err != nil {
		return err
	}
	scope := param(s, "scope")
	if err := requireField(s, "a scope (a URL prefix such as az://container/path/)", scope); err != nil {
		return err
	}
	ok := false
	for _, p := range AzureSchemes {
		if strings.HasPrefix(scope, p) && len(scope) > len(p) {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("azure connection %q: scope %q must start with %s and name a container", s.Name, scope, strings.Join(AzureSchemes, ", "))
	}
	if err := storageurl.Validate(scope); err != nil {
		return fmt.Errorf("azure connection %q: scope must be a plain URL prefix (no wildcards, query string or SAS token): %w", s.Name, err)
	}
	// The credential is sent to the host the scope names, so that host must be
	// the account's own endpoint (abfss://c@victim.dfs.evil.example/ is refused).
	scopeAcct, err := storageurl.AzureAccount(scope)
	if err != nil {
		return fmt.Errorf("azure connection %q: %w", s.Name, err)
	}
	if err := unknownOptions(s, "sign_in", "account_name", "tenant_id"); err != nil {
		return err
	}
	if err := oneOf("sign_in", opt(s, "sign_in"), azureAuths...); err != nil {
		return err
	}
	acct := opt(s, "account_name")
	if acct != "" && !azureAccountR.MatchString(acct) {
		return fmt.Errorf("azure connection %q: account name %q is not a storage account name (3 to 24 lowercase letters and digits)", s.Name, acct)
	}
	if acct != "" && scopeAcct != "" && acct != scopeAcct {
		return fmt.Errorf("azure connection %q: the scope names the account %q but account_name is %q", s.Name, scopeAcct, acct)
	}
	tenant := opt(s, "tenant_id")
	if tenant != "" && !azureTenantR.MatchString(tenant) {
		return fmt.Errorf("azure connection %q: tenant id %q is not a tenant id or domain", s.Name, tenant)
	}
	switch azureAuth(s) {
	case azureAuthAccountKey:
		if acct == "" {
			return fmt.Errorf("azure connection %q needs the storage account name (option account_name) to sign in with an account key", s.Name)
		}
	case azureAuthServicePrincipal:
		if acct == "" {
			return fmt.Errorf("azure connection %q needs the storage account name (option account_name) to sign in as a service principal", s.Name)
		}
		if tenant == "" {
			return fmt.Errorf("azure connection %q needs the tenant id (option tenant_id) to sign in as a service principal", s.Name)
		}
	}
	return nil
}

func (t azureType) SecretSteps(s session.Spec, c secrets.Credential) ([]session.SecretStep, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if c.Secret == "" {
		return nil, fmt.Errorf("azure connection %q needs its %s", s.Name, map[string]string{
			azureAuthConnectionString: "connection string",
			azureAuthAccountKey:       "account key",
			azureAuthServicePrincipal: "client secret",
		}[azureAuth(s)])
	}
	if err := noControl("secret", c.Secret); err != nil {
		return nil, fmt.Errorf("azure connection %q: %w", s.Name, err)
	}
	scope := session.SecretParam{Key: "SCOPE", Value: param(s, "scope")}
	var params []session.SecretParam
	switch azureAuth(s) {
	case azureAuthConnectionString:
		// The string's own account must be the one the scope and the account
		// name option say: the credential must not be sent to another account's
		// endpoint by a scope that names it.
		if got := connStringAccount(c.Secret); got != "" {
			want := opt(s, "account_name")
			if a, _ := storageurl.AzureAccount(param(s, "scope")); a != "" {
				want = a
			}
			if want != "" && got != want {
				return nil, fmt.Errorf("azure connection %q: the connection string is for a different storage account than the scope and account name say", s.Name)
			}
		}
		params = []session.SecretParam{{Key: "CONNECTION_STRING", Value: c.Secret, Secret: true}, scope}
	case azureAuthAccountKey:
		if !azureKeyR.MatchString(c.Secret) {
			return nil, fmt.Errorf("azure connection %q: the account key is not a base64 key (use the connection string sign-in for anything else)", s.Name)
		}
		// The key is base64 (no ';' or '='-sequences that could add a field to
		// the string); the account name was validated above.
		cs := "DefaultEndpointsProtocol=https;AccountName=" + opt(s, "account_name") + ";AccountKey=" + c.Secret + ";EndpointSuffix=" + azureEndpointSuffixDefault
		params = []session.SecretParam{{Key: "CONNECTION_STRING", Value: cs, Secret: true}, scope}
	case azureAuthServicePrincipal:
		if c.KeyID == "" {
			return nil, fmt.Errorf("azure connection %q needs the client id of the service principal", s.Name)
		}
		if !azureClientR.MatchString(c.KeyID) {
			return nil, fmt.Errorf("azure connection %q: client id %q is not a client id", s.Name, c.KeyID)
		}
		params = []session.SecretParam{
			{Key: "PROVIDER", Value: "service_principal"},
			{Key: "TENANT_ID", Value: opt(s, "tenant_id")},
			{Key: "CLIENT_ID", Value: c.KeyID},
			{Key: "CLIENT_SECRET", Value: c.Secret, Secret: true},
			{Key: "ACCOUNT_NAME", Value: opt(s, "account_name")},
			scope,
		}
	}
	return []session.SecretStep{{Name: secretName(s.Name), Type: "azure", Params: params}}, nil
}

// AttachSQL: a storage connection attaches nothing.
func (t azureType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	return []string{}, nil
}

func (azureType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	return probeScope(ctx, sess, param(s, "scope"))
}

func (azureType) Info() TypeInfo {
	return TypeInfo{
		Name: "azure", Label: "Azure Blob / Data Lake",
		Fields: []Field{
			{Name: "scope", Label: "Scope", Kind: KindURL, Required: true, Help: "A URL prefix such as az://container/path/ or abfss://container@account.dfs.core.windows.net/path/. Reads are allowed only inside it."},
			{Name: "sign_in", Label: "Sign in with", Kind: KindEnum, Choices: azureAuths, Default: azureDefaultAuth, Option: true, Prominent: true, Help: "Account key and service principal also need the account name; a service principal needs the tenant id. Ambient (az login) credentials are never used."},
			{Name: "account_name", Label: "Storage account", Kind: KindText, Option: true, Help: "Not needed for a connection string."},
			{Name: "tenant_id", Label: "Tenant id", Kind: KindText, Option: true, Help: "Service principal only."},
		},
		Credentials: []Field{
			{Name: "key_id", Label: "Client id", Kind: KindText, PerPerson: true, Help: "Service principal only."},
			{Name: "secret", Label: "Secret", Kind: KindText, Required: true, Secret: true, PerPerson: true, Help: "The connection string, the account key or the client secret, matching Sign in with."},
		},
		Extensions:        []string{"azure"},
		Capabilities:      Capabilities{Catalog: false, Writable: false, ReadOnlyAttach: false, Remote: true, Test: true},
		FingerprintFields: []string{"type", "scope", "option:sign_in", "option:account_name", "option:tenant_id"},
		SignInMethods:     []string{"keychain", "env", "prompt"},
	}
}

// connStringAccount returns the AccountName of an Azure connection string ("" when
// it has none).
func connStringAccount(cs string) string {
	for _, part := range strings.Split(cs, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "AccountName") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
