package awssso

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // the AWS CLI and SDK name the token cache by SHA-1; not a security use
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
)

// Kind is the shape of an SSO profile.
type Kind string

const (
	// KindSession is a profile with sso_session (a refresh token is possible).
	KindSession Kind = "sso-session"
	// KindLegacy is a profile with sso_start_url in the profile itself.
	KindLegacy Kind = "legacy"
)

const defaultScope = "sso:account:access"

var (
	profileNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+@-]{0,63}$`)
	regionRE      = regexp.MustCompile(`^[a-z]{2,5}(-[a-z]+){1,2}-[0-9]{1,2}$`)
	accountRE     = regexp.MustCompile(`^[0-9]{12}$`)
	roleNameRE    = regexp.MustCompile(`^[A-Za-z0-9_+=,.@-]{1,64}$`)
	scopeRE       = regexp.MustCompile(`^[A-Za-z0-9:_./-]{1,64}$`)
)

// Profile is a validated SSO profile. None of its fields is a secret, but the
// account, role and start URL stay out of messages and logs (LogValue names the
// profile only).
type Profile struct {
	Name      string
	Kind      Kind
	StartURL  string
	Region    string // sso_region: the only thing endpoints are derived from
	AccountID string
	RoleName  string
	// SessionName is the sso-session section name (KindSession only).
	SessionName string
	// Scopes are the sso_registration_scopes of the session (KindSession only).
	Scopes []string
}

// String names the profile only.
func (p Profile) String() string { return p.Name }

// GoString keeps %#v to the name too.
func (p Profile) GoString() string { return p.Name }

// LogValue implements slog.LogValuer: the profile name only.
func (p Profile) LogValue() slog.Value { return slog.StringValue(p.Name) }

// ValidProfileName reports whether name is a safe subset of AWS profile names.
func ValidProfileName(name string) bool { return profileNameRE.MatchString(name) }

// cacheKey is what the AWS CLI and SDK hash to name the token cache file: the
// sso-session name, or the start URL for a legacy profile.
func (p Profile) cacheKey() string {
	if p.Kind == KindSession {
		return p.SessionName
	}
	return p.StartURL
}

// fingerprint identifies the identity a profile resolves to, so a cache entry
// is dropped when the profile's account, role or session changes.
func (p Profile) fingerprint() string {
	return strings.Join([]string{p.Name, string(p.Kind), p.cacheKey(), p.Region, p.AccountID, p.RoleName}, "\x00")
}

// tokenPath is ~/.aws/sso/cache/<sha1 of the cache key>.json under home. The
// name is a hash, so a crafted session name cannot traverse the path.
func tokenPath(home, key string) string {
	sum := sha1.Sum([]byte(key)) //nolint:gosec // see import
	return filepath.Join(home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json")
}

// LoadProfile reads one profile from the shared AWS config (AWS_CONFIG_FILE and
// AWS_SHARED_CREDENTIALS_FILE from the Provider's getenv, else under its home).
// Only SSO profiles are accepted.
func (p *Provider) LoadProfile(ctx context.Context, name string) (Profile, error) {
	if !ValidProfileName(name) {
		return Profile{}, &ProfileError{Profile: "(invalid name)", Problem: "has a name Vibe does not accept"}
	}
	cfgFiles, credFiles, err := p.configFiles()
	if err != nil {
		return Profile{}, err
	}
	cfg, err := config.LoadSharedConfigProfile(ctx, name, func(o *config.LoadSharedConfigOptions) {
		o.ConfigFiles, o.CredentialsFiles = cfgFiles, credFiles
	})
	if err != nil {
		var nf config.SharedConfigProfileNotExistError
		if errors.As(err, &nf) {
			return Profile{}, &ProfileError{Profile: name, Problem: "was not found in your AWS config"}
		}
		return Profile{}, &ProfileError{Profile: name, Problem: "could not be read from your AWS config"}
	}
	prof, err := profileFrom(name, cfg)
	if err != nil {
		return Profile{}, err
	}
	if prof.Kind == KindSession {
		prof.Scopes = registrationScopes(cfgFiles, prof.SessionName)
	}
	return prof, nil
}

func (p *Provider) configFiles() (cfg, cred []string, err error) {
	cfgFile, credFile := p.getenv("AWS_CONFIG_FILE"), p.getenv("AWS_SHARED_CREDENTIALS_FILE")
	if cfgFile == "" || credFile == "" {
		if p.home == "" {
			return nil, nil, errors.New("no home directory to find the AWS config in")
		}
	}
	if cfgFile == "" {
		cfgFile = filepath.Join(p.home, ".aws", "config")
	}
	if credFile == "" {
		credFile = filepath.Join(p.home, ".aws", "credentials")
	}
	return []string{cfgFile}, []string{credFile}, nil
}

// otherSources names the credential settings a profile must not carry.
func otherSources(cfg config.SharedConfig) []string {
	var out []string
	for _, s := range []struct {
		set  bool
		name string
	}{
		{cfg.CredentialProcess != "", "credential_process"},
		{cfg.RoleARN != "", "role_arn"},
		{cfg.SourceProfileName != "", "source_profile"},
		{cfg.CredentialSource != "", "credential_source"},
		{cfg.WebIdentityTokenFile != "", "web_identity_token_file"},
		{cfg.LoginSession != "", "login_session"},
		{cfg.Credentials.HasKeys(), "static keys"},
	} {
		if s.set {
			out = append(out, s.name)
		}
	}
	return out
}

func profileFrom(name string, cfg config.SharedConfig) (Profile, error) {
	bad := func(problem string) (Profile, error) {
		return Profile{}, &ProfileError{Profile: name, Problem: problem}
	}
	if cfg.SSOSessionName == "" && cfg.SSOStartURL == "" {
		return bad("is not an AWS IAM Identity Center (SSO) profile")
	}
	if other := otherSources(cfg); len(other) > 0 {
		return bad("mixes SSO with " + strings.Join(other, ", ") + "; Vibe accepts SSO-only profiles")
	}
	prof := Profile{Name: name, Kind: KindLegacy, StartURL: cfg.SSOStartURL, Region: cfg.SSORegion,
		AccountID: cfg.SSOAccountID, RoleName: cfg.SSORoleName}
	if cfg.SSOSessionName != "" {
		s := cfg.SSOSession
		if s == nil {
			return bad("names an sso_session that is not in your AWS config")
		}
		if (cfg.SSOStartURL != "" && cfg.SSOStartURL != s.SSOStartURL) || (cfg.SSORegion != "" && cfg.SSORegion != s.SSORegion) {
			return bad("sets an sso_start_url or sso_region that differs from its sso_session")
		}
		prof.Kind, prof.SessionName = KindSession, strings.TrimSpace(cfg.SSOSessionName)
		prof.StartURL, prof.Region = s.SSOStartURL, s.SSORegion
	}
	if prof.StartURL == "" || prof.Region == "" || prof.AccountID == "" || prof.RoleName == "" {
		return bad("is missing sso_start_url, sso_region, sso_account_id or sso_role_name")
	}
	if !regionRE.MatchString(prof.Region) {
		return bad("has an sso_region that is not an AWS region")
	}
	if !accountRE.MatchString(prof.AccountID) || !roleNameRE.MatchString(prof.RoleName) {
		return bad("has an sso_account_id or sso_role_name that is not valid")
	}
	if u, err := url.Parse(prof.StartURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return bad("has an sso_start_url that is not an https URL")
	} else if !startHostOK(u.Host, prof.Region) {
		return bad("has an sso_start_url that is not an AWS access portal (<id>.awsapps.com, <id>.awsapps.cn or <id>.portal.<region>.app.aws)")
	}
	return prof, nil
}

const maxConfigBytes = 4 << 20

// registrationScopes reads sso_registration_scopes of the [sso-session NAME]
// section (the SDK's shared-config model does not expose it). Later files win.
// Invalid scopes are dropped; none left means the default.
func registrationScopes(files []string, session string) []string {
	var raw string
	for _, f := range files {
		if v, ok := scanSessionKey(f, session, "sso_registration_scopes"); ok {
			raw = v
		}
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); scopeRE.MatchString(s) && len(out) < 10 {
			out = append(out, s)
		}
	}
	return out
}

func scanSessionKey(file, session, key string) (string, bool) {
	f, err := os.Open(file) //nolint:gosec // the person's own AWS config
	if err != nil {
		return "", false
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(io.LimitReader(f, maxConfigBytes))
	in, val, found := false, "", false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[':
			in = isSessionHeader(line, session)
		case in:
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
				val, found = strings.TrimSpace(v), true
			}
		}
	}
	return val, found
}

func isSessionHeader(line, session string) bool {
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return false
	}
	fields := strings.Fields(line[1:end])
	return len(fields) == 2 && fields[0] == "sso-session" && fields[1] == session
}

// startHostOK: the start URL is where the person signs in, so it must be an AWS
// access-portal host, as the device page is. Lower case, no port.
func startHostOK(host, region string) bool {
	if !lexicallySafe(host) {
		return false
	}
	for _, s := range []string{".awsapps.com", ".awsapps.cn", ".portal." + region + ".app.aws"} {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}
