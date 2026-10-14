package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Size caps and timeouts for each kind of fetch.
const (
	maxNotesBytes     = 256 << 10
	maxChecksumsBytes = 64 << 10
	maxScriptBytes    = 1 << 20

	checkTimeout    = 10 * time.Second
	downloadTimeout = 60 * time.Second
)

// ErrTooLarge means a response exceeded its cap.
var ErrTooLarge = errors.New("response is larger than allowed")

// checkURL is the one gate every URL passes before a request: https (http only
// when explicitly allowed), a host, no credentials in it.
func (u *Updater) checkURL(raw string) (*url.URL, error) {
	p, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if p.Scheme != "https" && !(u.insecure && p.Scheme == "http") {
		return nil, fmt.Errorf("refusing non-https URL %q", p.Scheme+"://"+p.Host)
	}
	if p.Host == "" || p.User != nil {
		return nil, fmt.Errorf("invalid URL host")
	}
	return p, nil
}

// hostAllowed reports whether a redirect (or the first hop) may use hostport. GitHub's own hosts
// match by (case-insensitive) hostname on the default https port only; a configured non-default
// base (tests, the dev hook) is trusted as exactly itself. extra names more allowed hostnames for
// one request (the notes fetch may reach api.github.com).
func (u *Updater) hostAllowed(hostport string, extra ...string) bool {
	host, port := strings.ToLower(hostport), ""
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		host, port = strings.ToLower(h), p
	}
	if port == "" || port == "443" {
		if u.hosts[host] {
			return true
		}
		for _, e := range extra {
			if host == e {
				return true
			}
		}
	}
	for _, b := range []struct{ cur, def string }{{u.webBase, defaultWebBase}, {u.apiBase, defaultAPIBase}} {
		if p, err := url.Parse(b.cur); err == nil && b.cur != b.def && strings.EqualFold(p.Host, hostport) {
			return true
		}
	}
	return false
}

// get fetches raw with a timeout and a size cap. With follow false a redirect
// is returned as is (status 3xx, empty body) instead of being followed.
func (u *Updater) get(ctx context.Context, raw string, limit int64, timeout time.Duration, follow bool, extraHosts ...string) (*http.Response, []byte, error) {
	if _, err := u.checkURL(raw); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	c := *u.client // copy: never mutate the injected client
	c.Jar = nil
	c.Timeout = timeout
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if !follow {
			return http.ErrUseLastResponse
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if _, err := u.checkURL(r.URL.String()); err != nil {
			return err
		}
		if !u.hostAllowed(r.URL.Host, extraHosts...) {
			return fmt.Errorf("refusing redirect to %s", r.URL.Host)
		}
		return nil
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp, nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > limit {
		return resp, nil, ErrTooLarge
	}
	return resp, body, nil
}

// getOK is get for a 200 body.
func (u *Updater) getOK(ctx context.Context, raw string, limit int64, timeout time.Duration, extraHosts ...string) ([]byte, error) {
	resp, body, err := u.get(ctx, raw, limit, timeout, true, extraHosts...)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return body, nil
}
