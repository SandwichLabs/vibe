package awssso

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const maxTokenFileBytes = 1 << 20

// tokenFile is the AWS CLI's SSO cache document. Only these fields are read or
// written; times are RFC3339, which is what the SDK parses.
type tokenFile struct {
	StartURL              string `json:"startUrl,omitempty"`
	Region                string `json:"region,omitempty"`
	AccessToken           string `json:"accessToken,omitempty"`
	ExpiresAt             string `json:"expiresAt,omitempty"`
	ClientID              string `json:"clientId,omitempty"`
	ClientSecret          string `json:"clientSecret,omitempty"`
	RegistrationExpiresAt string `json:"registrationExpiresAt,omitempty"`
	RefreshToken          string `json:"refreshToken,omitempty"`
}

// refreshable reports whether the SDK can refresh this token: it needs all of
// refreshToken, clientId and clientSecret.
func (t tokenFile) refreshable() bool {
	return t.RefreshToken != "" && t.ClientID != "" && t.ClientSecret != ""
}

func (t tokenFile) expires() (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, t.ExpiresAt)
	return at, err == nil && !at.IsZero()
}

// readToken reads the cache file at path. ok is false when there is no usable
// token (absent, unreadable, malformed, or missing accessToken/expiresAt); the
// reason is deliberately not reported (it could carry a path).
func readToken(path string) (t tokenFile, ok bool) {
	f, err := os.Open(path) //nolint:gosec // the person's own SSO cache
	if err != nil {
		return tokenFile{}, false
	}
	defer f.Close() //nolint:errcheck
	b, err := io.ReadAll(io.LimitReader(f, maxTokenFileBytes))
	if err != nil || json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return tokenFile{}, false
	}
	if _, valid := t.expires(); !valid {
		return tokenFile{}, false
	}
	return t, true
}

// SessionState is what the token cache says, with no network.
type SessionState struct {
	// SignedIn: there is a token that is valid now, or one that has expired but
	// can be refreshed (the next Credentials call refreshes it unprompted).
	SignedIn bool
	// Expires is when the cached access token expires (zero when none).
	Expires time.Time
	// Refreshable: the token has expired but a refresh token can renew it.
	Refreshable bool
	// Reason is why not signed in: ReasonMissing or ReasonExpired.
	Reason Reason
}

// Session reads the token cache for the profile, with no network.
func (p *Provider) Session(prof Profile) SessionState {
	return p.stateOf(prof)
}

func (p *Provider) stateOf(prof Profile) SessionState {
	t, ok := readToken(tokenPath(p.home, prof.cacheKey()))
	if !ok {
		return SessionState{Reason: ReasonMissing}
	}
	exp, _ := t.expires()
	switch {
	case p.now().Before(exp):
		return SessionState{SignedIn: true, Expires: exp}
	case prof.Kind == KindSession && t.refreshable():
		return SessionState{SignedIn: true, Expires: exp, Refreshable: true}
	default:
		return SessionState{Expires: exp, Reason: ReasonExpired}
	}
}

// writeToken writes t to path the way the AWS CLI's cache is kept private: the
// directory is created 0700, the document goes to a new 0600 file made with
// O_EXCL in the same directory, and is renamed over the target. A symlink at
// the target is replaced, never followed.
func (p *Provider) writeToken(path string, t tokenFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("couldn't create the AWS SSO cache folder")
	}
	body, err := json.Marshal(t)
	if err != nil {
		return errors.New("couldn't encode the AWS SSO token")
	}
	tmp, err := p.createExclusive(path)
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return errors.New("couldn't write the AWS SSO token cache")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.New("couldn't write the AWS SSO token cache")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("couldn't write the AWS SSO token cache")
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("couldn't replace the AWS SSO token cache")
	}
	ok = true
	return nil
}

// createExclusive makes the temp file beside path: O_CREATE|O_EXCL, 0600, a name
// nobody can predict. A name that already exists (a planted file or symlink) is
// never opened; another name is tried.
func (p *Provider) createExclusive(path string) (*os.File, error) {
	for range 5 {
		name := path + ".tmp-" + p.tmpSuffix()
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // see doc
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, errors.New("couldn't create a file in the AWS SSO cache folder")
		}
	}
	return nil, fmt.Errorf("couldn't create a temporary file in the AWS SSO cache folder")
}

func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
