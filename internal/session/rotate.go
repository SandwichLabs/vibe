package session

// Rotating a storage secret before the credential it was made from ends
// (#131, D9). A sign-in method such as aws-sso hands out short-lived keys
// (secrets.Credential.Expires); the hub keeps the deadline of each secret it
// made from one and, before a statement runs, renews the secrets that are close
// to it: new keys are fetched with no lock held (the network call can take
// seconds, and adminMu serialises every ATTACH and DETACH), then, under adminMu,
// the entry is checked to be the very one the keys were fetched for, and the
// secret is replaced in place (CREATE OR REPLACE, same name, never dropped), so
// nothing that reads with it ever reads anonymously. When no new keys can be had
// the connection fails closed: its dependents are detached, the secret dropped,
// and the status says the person must sign in.

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/storageurl"
)

const (
	// rotateBefore is how long before a credential ends it is renewed. The wall
	// clock is compared (a timer's monotonic clock stops while a laptop sleeps).
	rotateBefore = 5 * time.Minute
	// rotateInterval is the least time between two attempts to renew one
	// connection's secret while its keys still work: a portal that is down is
	// asked once per interval, not once per statement.
	rotateInterval = 30 * time.Second
	// rotateTimeout bounds a rotation started by a failed read, whose own
	// context may be spent.
	rotateTimeout = 15 * time.Second
)

// errRotateSkipped means no rotation changed anything: not due, throttled, the
// entry changed meanwhile, or the attempt failed but the old keys still work.
var errRotateSkipped = errors.New("rotation skipped")

// rotation is one rotation in flight; callers that arrive meanwhile wait on it.
type rotation struct {
	done chan struct{}
	err  error
}

// rotateDue renews every attached secret whose credential ends within
// rotateBefore. It returns the failures that left a connection signed out.
func (h *hub) rotateDue(ctx context.Context) error {
	now := h.clock()
	h.mu.RLock()
	var due []string
	for n, e := range h.conns {
		if e.attach && !e.expires.IsZero() && !now.Before(e.expires.Add(-rotateBefore)) {
			due = append(due, n)
		}
	}
	h.mu.RUnlock()
	sort.Strings(due)
	var errs error
	for _, n := range due {
		if err := h.rotate(ctx, n, false); err != nil && !errors.Is(err, errRotateSkipped) {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// rotateAll renews the attached secrets that have a deadline and whose scope
// covers url (every one when the error named no URL), because a read failed
// with an expired-token answer. It never fails the caller's statement.
func (h *hub) rotateAll(ctx context.Context, url string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rotateTimeout)
	defer cancel()
	h.mu.RLock()
	var names, all []string
	for n, e := range h.conns {
		if e.attach && !e.expires.IsZero() {
			all = append(all, n)
			if coversURL(e.spec.Params["scope"], url) {
				names = append(names, n)
			}
		}
	}
	h.mu.RUnlock()
	if len(names) == 0 {
		names = all // the URL matched no scope: do not guess, the interval bounds it
	}
	sort.Strings(names)
	for _, n := range names {
		_ = h.rotate(ctx, n, true)
	}
}

// readErr is wrapErr for a failed statement: an expired-token answer from
// object storage also starts one (rate-limited) rotation, so the next
// statement can work when the sign-in has fresh keys to give.
func (h *hub) readErr(ctx context.Context, msg string, err error) error {
	if IsExpiredToken(err.Error()) {
		h.rotateAll(ctx, errorURL(err.Error()))
	}
	return h.wrapErr(msg, err)
}

// plan is what rotate decided under the lock.
type rotatePlan struct {
	e    *entry
	gen  int
	spec Spec
	mine *rotation
	wait *rotation
}

// startRotation decides, under mu, whether this caller rotates, waits for one
// in flight, or has nothing to do (the zero plan).
func (h *hub) startRotation(name string, forced bool) rotatePlan {
	now := h.clock()
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.conns[name]
	if e == nil || !e.attach || e.expires.IsZero() {
		return rotatePlan{}
	}
	if e.rot != nil {
		return rotatePlan{wait: e.rot}
	}
	expired := !now.Before(e.expires)
	if !forced && now.Before(e.expires.Add(-rotateBefore)) {
		return rotatePlan{} // not due
	}
	if (!expired || e.closeRefused) && !e.lastRotate.IsZero() && now.Sub(e.lastRotate) < rotateInterval {
		return rotatePlan{}
	}
	e.lastRotate = now
	e.rot = &rotation{done: make(chan struct{})}
	return rotatePlan{e: e, gen: e.secretGen, spec: e.spec, mine: e.rot}
}

// rotate renews name's secret. forced is set by a read that failed with an
// expired-token answer: the keys are bad whatever the clock says. A nil error
// means the secret was renewed or there was nothing to do; errRotateSkipped,
// that nothing changed; any other error, that the connection was signed out.
func (h *hub) rotate(ctx context.Context, name string, forced bool) error {
	p := h.startRotation(name, forced)
	switch {
	case p.wait != nil:
		select {
		case <-p.wait.done:
			return p.wait.err
		case <-ctx.Done():
			return ctx.Err()
		}
	case p.mine == nil:
		return errRotateSkipped
	}
	// New keys, with no lock held.
	cred, rerr := p.spec.Credential(secrets.WithFresh(ctx))
	changed, err := h.applyRotation(ctx, name, p, cred, rerr)
	h.mu.Lock()
	p.e.rot = nil
	h.mu.Unlock()
	p.mine.err = err
	close(p.mine.done)
	if changed {
		h.notifyStatus(name)
	}
	return err
}

// applyRotation is the part of a rotation that changes DuckDB, under adminMu.
// It reads the entry again there: whatever the keys were fetched for may have
// been unregistered, registered anew, or preflighted since.
func (h *hub) applyRotation(ctx context.Context, name string, p rotatePlan, cred secrets.Credential, rerr error) (bool, error) {
	if rerr != nil && (errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded)) {
		return false, errRotateSkipped
	}
	ctx = context.WithoutCancel(ctx) // a half-applied rotation is worse than a late one
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	cur, att := h.entryOf(name)
	h.mu.RLock()
	same := cur == p.e && att && p.e.secretGen == p.gen
	expires := p.e.expires
	h.mu.RUnlock()
	if !same {
		return false, errRotateSkipped // never act on what was read before adminMu
	}
	if rerr == nil {
		rerr = h.replaceSecrets(ctx, p, cred)
		if rerr == nil {
			return false, nil
		}
	}
	var nc *secrets.ErrNeedsCredentials
	if !errors.As(rerr, &nc) && h.clock().Before(expires) {
		// The keys it has still work (whatever asked for the rotation): keep them
		// and ask again after the interval.
		h.logger.Warn("could not renew a storage credential yet", "name", name)
		return false, errRotateSkipped
	}
	return h.signOut(ctx, name, p.e, rerr)
}

// replaceSecrets rebuilds the connection's secrets from cred and replaces them
// in place. The caller holds adminMu and has checked the entry.
func (h *hub) replaceSecrets(ctx context.Context, p rotatePlan, cred secrets.Credential) error {
	t, err := h.types.Get(p.spec.Type)
	if err != nil {
		return err
	}
	ct, ok := t.(CredentialedType)
	if !ok {
		return errors.New("the connection has no credential to rotate")
	}
	steps, err := ct.SecretSteps(p.spec, cred)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(steps))
	for _, st := range steps {
		names = append(names, st.Name)
	}
	if !slices.Equal(names, p.e.secrets) {
		return errors.New("the connection's secrets changed shape")
	}
	h.redactor.AddCredential(cred)
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get admin connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	for _, st := range steps {
		if err := h.createSecretOn(ctx, conn, st.Name, st.Type, st.Params); err != nil {
			return err
		}
	}
	h.mu.Lock()
	p.e.expires = cred.Expires
	p.e.secretGen++
	p.e.closeRefused = false
	h.mu.Unlock()
	return nil
}

// signOut is the fail-closed path: detach what reads with the secret, drop it,
// and say the connection needs a sign-in. A dependent that cannot be detached
// keeps the secret (the old keys end by themselves; nothing reads bare). The
// caller holds adminMu. It returns whether the status changed, and the error
// to report.
func (h *hub) signOut(ctx context.Context, name string, e *entry, cause error) (bool, error) {
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return false, errRotateSkipped
	}
	defer conn.Close() //nolint:errcheck
	if derr := h.detachDependents(ctx, conn, name); derr != nil {
		// Ended keys are normally retried at once, but not while this keeps failing.
		h.mu.Lock()
		e.closeRefused = true
		h.mu.Unlock()
		h.logger.Warn("storage credential not renewed, and a dependent could not be detached", "name", name)
		return false, errRotateSkipped
	}
	h.dropSecretsOn(ctx, conn, e.secrets)
	h.mu.Lock()
	e.attach, e.secrets, e.expires = false, nil, time.Time{}
	h.mu.Unlock()
	kind, msg := credentialKind(cause)
	msg = h.redact(msg)
	h.setStatusCause(name, kind, msg, cause)
	h.InvalidateSchema("")
	var nc *secrets.ErrNeedsCredentials
	if errors.As(cause, &nc) {
		return true, fmt.Errorf("the connection %q needs sign-in: %s", name, msg)
	}
	return true, fmt.Errorf("the connection %q could not renew its credentials: %s", name, msg)
}

// notifyStatus tells the status listener that name changed on its own (not
// because a caller asked), so a UI can refresh. No lock is held.
func (h *hub) notifyStatus(name string) {
	if f := h.cfg.onStatus; f != nil {
		f(name)
	}
}

// IsExpiredToken reports whether an error from object storage says the session
// token in the request is expired or invalid: httpfs's "(HTTP 403 ...)" followed
// by the S3 code ExpiredToken or InvalidTokenId (what S3 and MinIO answer for a
// lapsed or revoked STS session). Only the driver's own message is read: DuckDB
// echoes the statement after it ("LINE 1: ..."), and identifiers or literals in
// a statement must not be able to pass for an answer from the store. It is the
// one rule the hub and the connection test share.
func IsExpiredToken(msg string) bool {
	low := strings.ToLower(msg)
	if i := strings.Index(low, "\nline "); i >= 0 {
		low = low[:i]
	}
	i := strings.Index(low, "(http 403")
	if i < 0 {
		return false
	}
	rest := low[i:]
	for _, k := range []string{"expiredtoken", "invalidtokenid", "security token included in the request is expired", "security token included in the request is invalid", "the provided token has expired"} {
		if strings.Contains(rest, k) {
			return true
		}
	}
	return false
}

var errorURLRE = regexp.MustCompile(`error reading '([^']+)'`)

// errorURL is the object URL httpfs names in a failed read ("" when none).
func errorURL(msg string) string {
	if m := errorURLRE.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	return ""
}

// coversURL reports whether a storage scope covers the URL of a failed read.
// httpfs names the request URL, which is the s3:// URL or the endpoint's own
// (https://bucket.s3.region.amazonaws.com/key, http://host/bucket/key), so the
// bucket and key prefix are matched in each shape. With no URL or no scope every
// scope does (rotateAll also falls back to all when none match).
func coversURL(scope, url string) bool {
	if url == "" || scope == "" {
		return true
	}
	sc, err := storageurl.Scope(scope)
	if err != nil {
		return false
	}
	if storageurl.Within(url, sc) {
		return true
	}
	su, err1 := neturl.Parse(sc)
	ru, err2 := neturl.Parse(url)
	if err1 != nil || err2 != nil || su.Host == "" || (ru.Scheme != "http" && ru.Scheme != "https") {
		return false
	}
	prefix := strings.TrimPrefix(su.Path, "/")                   // "" or "p/q/"
	return strings.HasPrefix(ru.Path, "/"+su.Host+"/"+prefix) || // path style
		(strings.HasPrefix(ru.Hostname(), su.Host+".") && strings.HasPrefix(ru.Path, "/"+prefix)) // virtual-hosted
}

func (h *hub) clock() time.Time {
	if f := h.cfg.clock; f != nil {
		return f()
	}
	return time.Now()
}
