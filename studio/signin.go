package main

// Signing in from the window (#131, step 8). StartSignIn runs the OIDC device
// flow for the AWS profile a connection uses, on a goroutine, and answers with
// what the person must do: open a page and check a code. The page is opened
// here, in Go, by the one call below, and only after the awssso host check has
// passed (awssso.DeviceLogin refuses any other address before it shows a
// prompt); the frontend never hands this code an address. One flow runs per
// profile at a time, and every flow ends with the workspace it was started in.
// Events carry names, the profile and a redacted sentence, never a token.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SignInPrompt is what the person is shown to finish a sign-in: the page that
// was opened and the code it should display. Neither is a secret.
type SignInPrompt struct {
	// Connection is the connection the flow was started from; Profile is what
	// is being signed in to (one flow per profile).
	Connection string    `json:"connection"`
	Profile    string    `json:"profile"`
	URL        string    `json:"url"`
	Code       string    `json:"code"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// signInFlow is one device flow in flight.
type signInFlow struct {
	profile string
	cancel  context.CancelFunc
	// ready closes when the prompt is known or the flow failed before it.
	ready  chan struct{}
	prompt SignInPrompt
	err    error
	// first is the connection that started the flow; conns are all that asked
	// for it (guarded by signInFlows.mu).
	first string
	conns map[string]bool
}

// signInFlows is the set of flows in flight, keyed by profile. The zero value
// is ready to use.
type signInFlows struct {
	mu    sync.Mutex
	byKey map[string]*signInFlow
	wg    sync.WaitGroup
}

// StartSignIn starts the AWS sign-in for the connection's profile and returns
// the prompt once AWS has issued the code. A flow already running for the
// profile is joined, not duplicated. The page is opened for the person; the
// result is for showing the code beside it.
func (a *App) StartSignIn(ref ConnectionRef) (*SignInPrompt, error) {
	ws, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	profile, err := workspace.SSOProfile(ws, ref.Name)
	if err != nil {
		return nil, err
	}
	f := a.flows.begin(profile, ref.Name, func(ctx context.Context, f *signInFlow) {
		a.runSignIn(ctx, ws, sess, f)
	}, a.ctxOrBackground())
	select {
	case <-f.ready:
	case <-a.ctxOrBackground().Done():
		return nil, a.ctxOrBackground().Err()
	}
	if f.err != nil {
		return nil, errors.New(a.redact(f.err.Error()))
	}
	p := f.prompt
	return &p, nil
}

// CancelSignIn stops the sign-in the connection's profile has running, if any.
func (a *App) CancelSignIn(ref ConnectionRef) { a.flows.cancelFor(ref.Name) }

// begin returns the flow for profile, starting run on a goroutine when none is
// in flight.
func (s *signInFlows) begin(profile, conn string, run func(context.Context, *signInFlow), parent context.Context) *signInFlow {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.byKey[profile]; f != nil {
		f.conns[conn] = true
		return f
	}
	if s.byKey == nil {
		s.byKey = map[string]*signInFlow{}
	}
	ctx, cancel := context.WithCancel(parent)
	f := &signInFlow{profile: profile, cancel: cancel, ready: make(chan struct{}), first: conn, conns: map[string]bool{conn: true}}
	s.byKey[profile] = f
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		run(ctx, f)
	}()
	return f
}

// finish removes the flow once it has ended.
func (s *signInFlows) finish(f *signInFlow) {
	s.mu.Lock()
	if s.byKey[f.profile] == f {
		delete(s.byKey, f.profile)
	}
	s.mu.Unlock()
}

func (s *signInFlows) cancelFor(conn string) {
	s.mu.Lock()
	var hit []*signInFlow
	for k, f := range s.byKey {
		if f.conns[conn] {
			hit = append(hit, f)
			delete(s.byKey, k) // a Sign in right after a cancel starts a new flow
		}
	}
	s.mu.Unlock()
	for _, f := range hit {
		f.cancel()
	}
}

// cancelAll ends every flow (the workspace is closing or switching).
func (s *signInFlows) cancelAll() {
	s.mu.Lock()
	hit := make([]*signInFlow, 0, len(s.byKey))
	for k, f := range s.byKey {
		hit = append(hit, f)
		delete(s.byKey, k)
	}
	s.mu.Unlock()
	for _, f := range hit {
		f.cancel()
	}
}

// runSignIn is the flow's goroutine: the device flow, then, if it worked,
// Preflight of every connection that uses the profile (a lake waiting on a
// storage connection follows through the hub) and the events.
func (a *App) runSignIn(ctx context.Context, ws workspace.Workspace, sess session.Session, f *signInFlow) {
	defer a.flows.finish(f)
	name := f.first
	shown := false
	_, err := workspace.DeviceSignIn(ctx, ws, name, func(p secrets.DevicePrompt) {
		f.prompt = SignInPrompt{Connection: name, Profile: f.profile, URL: p.URL, Code: p.Code, ExpiresAt: p.ExpiresAt}
		shown = true
		close(f.ready)
		a.openVerificationURL(p.URL)
	})
	if err != nil {
		if !shown {
			f.err = err
			close(f.ready)
		}
		if ctx.Err() == nil {
			a.emitEvent("signin:failed", map[string]any{"name": name, "profile": f.profile, "message": a.redact(err.Error())})
		}
		return
	}
	for _, n := range workspace.SSOConnections(ws, f.profile) {
		if ctx.Err() != nil || !a.stillOpen(ws) {
			return // the workspace was closed meanwhile
		}
		pctx, cancel := context.WithTimeout(ctx, connTimeout)
		sess.Preflight(pctx, n)
		cancel()
		a.emitStatus(sess, n)
	}
	a.emitEvent("signin:done", map[string]any{"name": name, "profile": f.profile})
}

// stillOpen reports whether ws is still the open workspace.
func (a *App) stillOpen(ws workspace.Workspace) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ws == ws
}

// openVerificationURL opens the sign-in page. u comes only from the flow, which
// has checked its host; it is never a value the frontend supplied.
func (a *App) openVerificationURL(u string) {
	if a.openURL != nil {
		a.openURL(u)
		return
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, u)
	}
}
