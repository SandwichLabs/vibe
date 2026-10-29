package repl

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

const (
	// MaxInput is the most bytes one Input call takes.
	MaxInput = 64 << 10
	// maxQueued is the most bytes waiting for the line editor.
	maxQueued = 256 << 10

	ctrlC = 0x03

	// interruptSeq clears the line the editor holds (Home, kill to end) and
	// ends it with a line of one private-use rune, which the loop recognises as
	// "the person pressed Ctrl+C". The line editor reports Ctrl+C and Ctrl+D
	// alike as io.EOF and keeps the half-typed line, so Ctrl+C is never handed
	// to it.
	interruptRune = ''
)

var (
	// interruptSeq is what an idle Ctrl+C becomes for the line editor.
	// It first leaves a bracketed paste the editor may think is open.
	interruptSeq = []byte("\x1b[201~\x01\x0b" + string(interruptRune) + "\r")

	// ErrInputTooLarge is returned by Input for a call over MaxInput bytes.
	ErrInputTooLarge = errors.New("console input exceeds the per-call limit")
	// ErrInputOverflow is returned by Input when the editor is not keeping up.
	ErrInputOverflow = errors.New("console input queue is full")
	// ErrClosed is returned by Input after Close.
	ErrClosed = errors.New("console session is closed")
)

// inputQueue is the byte stream the line editor reads: a bounded queue that
// blocks the reader until bytes arrive or the queue is closed.
type inputQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newInputQueue() *inputQueue {
	q := &inputQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// write queues p, or refuses all of it when the queue would pass its cap.
func (q *inputQueue) write(p []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	if len(q.buf)+len(p) > maxQueued {
		return ErrInputOverflow
	}
	q.buf = append(q.buf, p...)
	q.cond.Broadcast()
	return nil
}

// Read blocks until there are bytes; after close it returns io.EOF.
func (q *inputQueue) Read(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.buf) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return 0, io.EOF
	}
	// Hand over at most one line at a time, so the editor never holds typed-ahead
	// lines a Ctrl+C could not discard: they wait here, where flush reaches them.
	end := min(len(q.buf), len(p))
	if i := bytes.IndexAny(q.buf[:end], "\r\n"); i >= 0 {
		end = i + 1
		if q.buf[i] == '\r' && end < len(q.buf) && q.buf[end] == '\n' && end < len(p) {
			end++
		}
	}
	n := copy(p, q.buf[:end])
	q.buf = q.buf[n:]
	return n, nil
}

// flush drops what is waiting for the editor.
func (q *inputQueue) flush() {
	q.mu.Lock()
	q.buf = nil
	q.mu.Unlock()
}

func (q *inputQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.buf = nil
	q.mu.Unlock()
	q.cond.Broadcast()
}

// Input feeds bytes the terminal produced (keys, pastes). Safe from any
// goroutine. Ctrl+C while a statement is being handled cancels it and discards
// what was typed ahead; Ctrl+C at the prompt clears the line being typed.
// Everything else goes to the line editor, minus the characters filter drops.
func (s *Session) Input(p []byte) error {
	if len(p) > MaxInput {
		return ErrInputTooLarge
	}
	if s.closed.Load() {
		return ErrClosed
	}
	s.inMu.Lock()
	defer s.inMu.Unlock()
	saved := s.flt.snapshot()
	p = s.flt.apply(p)
	out := make([]byte, 0, len(p))
	for _, b := range p {
		if b != ctrlC {
			out = append(out, b)
			continue
		}
		if cancel := s.cancel.Load(); cancel != nil {
			s.interrupted.Store(true)
			s.dropRest.Store(true)
			s.in.flush()
			// The flush may have eaten the end of a paste: close it for the editor.
			out = append(out[:0], pasteEnd...)
			s.flt.paste = false
			(*cancel)()
			continue
		}
		out = append(out, interruptSeq...)
	}
	if err := s.in.write(out); err != nil {
		s.flt.restore(saved) // refused: the caller sends these bytes again
		return err
	}
	return nil
}
