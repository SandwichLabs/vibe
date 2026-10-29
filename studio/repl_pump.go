package main

import (
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/repl"
)

const (
	// pumpCoalesce is how long output waits to be batched into one frame.
	pumpCoalesce = 16 * time.Millisecond
	// pumpFrameCap is the most bytes one frame carries.
	pumpFrameCap = 64 << 10
	// pumpWindow is the most bytes that may be written and not yet
	// acknowledged by the terminal; past it the session's writes block.
	pumpWindow = 1 << 20
	// pumpMaxUnackedFrames bounds the frames in flight.
	pumpMaxUnackedFrames = 32
	// pumpReaderGone is how long the terminal may stay unattached, or leave
	// frames unacknowledged, before the console is ended.
	pumpReaderGone = 30 * time.Second
)

// errReaderGone is what a blocked write returns once the terminal has stopped
// reading.
var errReaderGone = errors.New("the console window stopped responding")

// replFrame is one "repl:out:<id>" event. A frame with Closed set is the last.
type replFrame struct {
	Seq    int    `json:"seq"`
	Data   string `json:"data"`
	Closed bool   `json:"closed,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// pumpLimits are the pump's tunables (tests shorten them).
type pumpLimits struct {
	coalesce  time.Duration
	frameCap  int
	window    int
	maxFrames int
	gone      time.Duration
}

func defaultPumpLimits() pumpLimits {
	return pumpLimits{pumpCoalesce, pumpFrameCap, pumpWindow, pumpMaxUnackedFrames, pumpReaderGone}
}

// outPump is the io.Writer a console writes its VT bytes to. It coalesces
// bytes into frames on a timer, cuts them at rune boundaries, and applies
// backpressure: it never drops bytes (a dropped escape sequence corrupts the
// terminal), it blocks the writer until the terminal acknowledges frames. A
// terminal that never attaches or stops acknowledging ends the console.
type outPump struct {
	emit   func(frame replFrame)
	lim    pumpLimits
	onGone func() // called once, in its own goroutine, when the reader is gone

	mu       sync.Mutex
	wake     chan struct{} // closed and replaced on every state change
	pending  []byte
	frames   []int // lengths of unacknowledged frames, oldest first
	unacked  int
	seq      int
	attached bool
	closed   bool
	flushT   *time.Timer
	watchT   *time.Timer
}

func newOutPump(emit func(replFrame), lim pumpLimits, onGone func()) *outPump {
	p := &outPump{emit: emit, lim: lim, onGone: onGone, wake: make(chan struct{})}
	p.mu.Lock()
	p.armWatchLocked()
	p.mu.Unlock()
	return p
}

// signalLocked wakes every blocked writer.
func (p *outPump) signalLocked() {
	close(p.wake)
	p.wake = make(chan struct{})
}

func (p *outPump) fullLocked() bool {
	return len(p.pending)+p.unacked >= p.lim.window || len(p.frames) >= p.lim.maxFrames
}

// Write queues p for the terminal, blocking while the window is full.
func (p *outPump) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for !p.closed && p.fullLocked() {
		ch := p.wake
		p.mu.Unlock()
		<-ch
		p.mu.Lock()
	}
	if p.closed {
		return 0, repl.ErrClosed
	}
	p.pending = append(p.pending, b...)
	p.armFlushLocked()
	return len(b), nil
}

func (p *outPump) armFlushLocked() {
	if p.flushT == nil && p.attached && len(p.pending) > 0 {
		p.flushT = time.AfterFunc(p.lim.coalesce, p.flush)
	}
}

// armWatchLocked (re)starts the reader-gone watchdog: it runs while the
// terminal is unattached or owes acknowledgements.
func (p *outPump) armWatchLocked() {
	if p.watchT != nil {
		p.watchT.Stop()
		p.watchT = nil
	}
	if p.closed || (p.attached && p.unacked == 0) {
		return
	}
	p.watchT = time.AfterFunc(p.lim.gone, p.watchFired)
}

func (p *outPump) watchFired() {
	p.mu.Lock()
	gone := !p.closed && (!p.attached || p.unacked > 0)
	p.mu.Unlock()
	if gone {
		p.close(errReaderGone.Error())
		if p.onGone != nil {
			go p.onGone()
		}
	}
}

// flush sends at most one frame, then re-arms if bytes remain.
func (p *outPump) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushT = nil
	if p.closed || !p.attached || len(p.pending) == 0 {
		return
	}
	if len(p.frames) >= p.lim.maxFrames || p.unacked >= p.lim.window {
		return // an ack re-arms
	}
	n := frameCut(p.pending, p.lim.frameCap, false)
	if n == 0 {
		return // an incomplete rune: wait for the rest
	}
	p.sendLocked(p.pending[:n])
	p.pending = append(p.pending[:0], p.pending[n:]...)
	p.armFlushLocked()
}

func (p *outPump) sendLocked(b []byte) {
	p.seq++
	p.frames = append(p.frames, len(b))
	p.unacked += len(b)
	p.armWatchLocked()
	p.emit(replFrame{Seq: p.seq, Data: string(b)})
}

// ack records that the terminal rendered frames up to and including seq.
// ack(0) attaches the terminal: nothing is emitted before it.
func (p *outPump) ack(seq int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if seq <= 0 {
		p.attached = true
	}
	// Frames are numbered from p.seq-len(p.frames)+1.
	first := p.seq - len(p.frames) + 1
	for len(p.frames) > 0 && first <= seq {
		p.unacked -= p.frames[0]
		p.frames = p.frames[1:]
		first++
	}
	p.armWatchLocked()
	p.signalLocked()
	p.armFlushLocked()
}

// close ends the stream: pending bytes are sent as a last frame, then one
// frame with Closed set. Idempotent.
func (p *outPump) close(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for _, t := range []*time.Timer{p.flushT, p.watchT} {
		if t != nil {
			t.Stop()
		}
	}
	p.flushT, p.watchT = nil, nil
	if p.attached && len(p.pending) > 0 && len(p.frames) < p.lim.maxFrames {
		for len(p.pending) > 0 {
			n := frameCut(p.pending, p.lim.frameCap, true)
			p.seq++
			p.emit(replFrame{Seq: p.seq, Data: string(p.pending[:n])})
			p.pending = p.pending[n:]
		}
	}
	p.pending = nil
	p.seq++
	p.emit(replFrame{Seq: p.seq, Closed: true, Reason: reason})
	p.signalLocked()
}

// frameCut is how many bytes of b go in the next frame: at most max, ending
// on a rune boundary (a split rune would reach the terminal as U+FFFD). With
// final false an incomplete rune at the very end waits for its other bytes.
func frameCut(b []byte, max int, final bool) int {
	n := min(len(b), max)
	if n < len(b) {
		for n > 0 && !utf8.RuneStart(b[n]) {
			n--
		}
	} else if !final {
		start := n - 1
		for start > 0 && n-start < utf8.UTFMax && !utf8.RuneStart(b[start]) {
			start--
		}
		if start >= 0 && !utf8.FullRune(b[start:n]) {
			n = start
		}
		return n
	}
	if n == 0 {
		n = min(len(b), max) // not UTF-8 at all: send it anyway
	}
	return n
}
