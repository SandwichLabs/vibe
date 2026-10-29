package repl

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

var (
	// ErrUseBusy: a statement is running (or the console is not at its prompt),
	// so the console's connection cannot change.
	ErrUseBusy = errors.New("a statement is running")
	// ErrUseMidStatement: the person is in the middle of a multi-line statement.
	ErrUseMidStatement = errors.New("a statement is half typed")
	// ErrUseLineNotEmpty: there is text typed at the prompt.
	ErrUseLineNotEmpty = errors.New("there is text typed at the prompt")
)

// useWait is how long Use waits for the loop to pick its request up.
const useWait = 3 * time.Second

// useRequest is one Use call, handed to the session's goroutine.
type useRequest struct {
	name  string
	taken atomic.Bool // set by whoever gets there first: the loop, or Use giving up
	reply chan useResult
}

type useResult struct {
	current string
	err     error
}

// Use switches the console to a connection from outside the line editor (the
// Query Explorer's "Run on" picker). "" means the workspace default. It never
// touches the session's state itself: it posts a request and a private key
// through the input queue, and the session's own goroutine serves it from the
// line editor's key callback, where the editor's real line is known. It
// refuses, with one of the ErrUse errors, rather than disturb anything: while
// a statement is running, in the middle of a multi-line statement, or with text
// on the line (an empty line, however it got empty, is fine). The switch runs
// like a statement: Ctrl+C cancels a slow dial. On success it prints
// "current: NAME" through the editor and returns the console's connection.
func (s *Session) Use(name string) (string, error) {
	if s.closed.Load() {
		return "", ErrClosed
	}
	if s.busy.Load() || s.running.Load() {
		return "", ErrUseBusy
	}
	req := &useRequest{name: name, reply: make(chan useResult, 1)}
	s.useMu.Lock()
	if s.useReq != nil {
		s.useMu.Unlock()
		return "", ErrUseBusy
	}
	s.useReq = req
	s.useMu.Unlock()
	withdraw := func() {
		s.useMu.Lock()
		if s.useReq == req {
			s.useReq = nil
		}
		s.useMu.Unlock()
	}
	// The key goes in with inMu held and only outside a bracketed paste, where the
	// editor takes every key as text and never calls the callback.
	s.inMu.Lock()
	st := s.flt.snapshot()
	var werr error
	if st.paste || len(st.carry) > 0 {
		werr = ErrUseBusy
	} else {
		werr = s.in.write([]byte(string(useRune)))
	}
	s.inMu.Unlock()
	if werr != nil {
		withdraw()
		return "", ErrUseBusy
	}
	timer := time.NewTimer(useWait)
	defer timer.Stop()
	select {
	case r := <-req.reply:
		return r.current, r.err
	case <-s.ctx.Done():
		return "", ErrClosed
	case <-timer.C:
		if req.taken.CompareAndSwap(false, true) {
			withdraw() // never picked up: the console was not at its prompt
			return "", ErrUseBusy
		}
	}
	select { // picked up: the switch itself is bounded by the dial timeout and Ctrl+C
	case r := <-req.reply:
		return r.current, r.err
	case <-s.ctx.Done():
		return "", ErrClosed
	}
}

// serveUse runs on the session's goroutine, inside the line editor's key
// callback, with line the editor's current line.
func (s *Session) serveUse(line string) {
	s.useMu.Lock()
	req := s.useReq
	s.useReq = nil
	s.useMu.Unlock()
	if req == nil || !req.taken.CompareAndSwap(false, true) {
		return // nobody is waiting any more
	}
	res := useResult{current: s.current}
	switch {
	case s.buf != "":
		res.err = ErrUseMidStatement
	case line != "":
		res.err = ErrUseLineNotEmpty
	default:
		res = s.switchForUse(req.name)
	}
	req.reply <- res
}

func (s *Session) switchForUse(name string) useResult {
	if name == "" {
		name = s.o.Sess.DefaultCatalog()
	}
	if name != s.current {
		ctx, cancel := s.beginRun() // Ctrl+C cancels the dial, as it cancels a statement
		err := s.switchToCtx(ctx, name)
		cancelled := ctx.Err() != nil
		s.endRun(cancel)
		if cancelled {
			return useResult{current: s.current, err: errors.New("cancelled")}
		}
		if err != nil {
			return useResult{current: s.current, err: fmt.Errorf("%s", errText(err))}
		}
	}
	// Through the editor, which clears its line, writes, and redraws the prompt.
	_, _ = s.term.Write([]byte(s.pal().dim("current: "+safe(s.current)) + "\n"))
	return useResult{current: s.current}
}

func errText(err error) string {
	var de *dotError
	if errors.As(err, &de) {
		return de.msg
	}
	return err.Error()
}

// failUse answers a pending Use request that the input flush just made
// unservable (its key was dropped) with the busy refusal, at once.
func (s *Session) failUse() {
	s.useMu.Lock()
	req := s.useReq
	s.useReq = nil
	s.useMu.Unlock()
	if req != nil && req.taken.CompareAndSwap(false, true) {
		req.reply <- useResult{err: ErrUseBusy}
	}
}
