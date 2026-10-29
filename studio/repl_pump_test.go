package main

import (
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/repl"
)

type frameLog struct {
	mu sync.Mutex
	fs []replFrame
}

func (l *frameLog) add(f replFrame) {
	l.mu.Lock()
	l.fs = append(l.fs, f)
	l.mu.Unlock()
}

func (l *frameLog) all() []replFrame {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]replFrame(nil), l.fs...)
}

func (l *frameLog) text() string {
	var sb strings.Builder
	for _, f := range l.all() {
		sb.WriteString(f.Data)
	}
	return sb.String()
}

func (l *frameLog) waitLen(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(l.all()) >= n }, 3*time.Second, 5*time.Millisecond)
}

func testLimits() pumpLimits {
	return pumpLimits{coalesce: 5 * time.Millisecond, frameCap: 64, window: 256, maxFrames: 8, gone: time.Minute}
}

func TestPumpHoldsOutputUntilAttached(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	_, err := p.Write([]byte("banner"))
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)
	assert.Empty(t, l.all(), "nothing before ReplAck(id, 0)")
	p.ack(0)
	l.waitLen(t, 1)
	assert.Equal(t, "banner", l.text())
}

func TestPumpCoalescesWrites(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	p.ack(0)
	for _, s := range []string{"a", "b", "c"} {
		_, err := p.Write([]byte(s))
		require.NoError(t, err)
	}
	l.waitLen(t, 1)
	assert.Equal(t, "abc", l.all()[0].Data)
	assert.Equal(t, 1, l.all()[0].Seq)
}

func TestPumpCutsAtRuneBoundaries(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	p.ack(0)
	in := strings.Repeat("é─日", 100) // 2, 3 and 3 bytes
	_, err := p.Write([]byte(in))
	require.NoError(t, err)
	acked := 0
	require.Eventually(t, func() bool {
		for _, f := range l.all() {
			if f.Seq > acked {
				acked = f.Seq
				p.ack(f.Seq)
			}
		}
		return l.text() == in
	}, 3*time.Second, 5*time.Millisecond)
	for i, f := range l.all() {
		assert.True(t, utf8.ValidString(f.Data), "frame %d", i)
		assert.LessOrEqual(t, len(f.Data), 64)
		assert.Equal(t, i+1, f.Seq)
	}
}

func TestPumpWaitsForTheRestOfARune(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	p.ack(0)
	_, _ = p.Write([]byte("a\xe2\x94"))
	l.waitLen(t, 1)
	assert.Equal(t, "a", l.all()[0].Data)
	_, _ = p.Write([]byte("\x80"))
	p.ack(1)
	l.waitLen(t, 2)
	assert.Equal(t, "─", l.all()[1].Data)
}

func TestPumpBackpressureBlocksUntilAcked(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	p.ack(0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 { // 20 x 64 bytes > the 256-byte window
			if _, err := p.Write([]byte(strings.Repeat("x", 64))); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
		t.Fatal("the writer was never held back")
	case <-time.After(150 * time.Millisecond):
	}
	acked := 0
	require.Eventually(t, func() bool {
		for _, f := range l.all() {
			if f.Seq > acked {
				acked = f.Seq
				p.ack(f.Seq)
			}
		}
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 3*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return len(l.text()) == 20*64 }, 3*time.Second, 5*time.Millisecond)
}

func TestPumpAckAccounting(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, pumpLimits{coalesce: time.Millisecond, frameCap: 4, window: 1 << 20, maxFrames: 100, gone: time.Minute}, nil)
	p.ack(0)
	_, _ = p.Write([]byte("aaaabbbbcccc"))
	l.waitLen(t, 3)
	p.mu.Lock()
	assert.Equal(t, 12, p.unacked)
	assert.Len(t, p.frames, 3)
	p.mu.Unlock()
	p.ack(2)
	p.mu.Lock()
	assert.Equal(t, 4, p.unacked)
	assert.Len(t, p.frames, 1)
	p.mu.Unlock()
	p.ack(3)
	p.mu.Lock()
	assert.Zero(t, p.unacked)
	p.mu.Unlock()
}

func TestPumpWriteAfterCloseFails(t *testing.T) {
	var l frameLog
	p := newOutPump(l.add, testLimits(), nil)
	p.ack(0)
	_, _ = p.Write([]byte("last words"))
	p.close("done")
	_, err := p.Write([]byte("x"))
	assert.ErrorIs(t, err, repl.ErrClosed)
	fs := l.all()
	require.Len(t, fs, 2)
	assert.Equal(t, "last words", fs[0].Data)
	assert.True(t, fs[1].Closed)
	assert.Equal(t, "done", fs[1].Reason)
	p.close("again")
	assert.Len(t, l.all(), 2, "close is idempotent")
}

func TestPumpUnattachedReaderIsGone(t *testing.T) {
	var l frameLog
	gone := make(chan struct{})
	lim := testLimits()
	lim.gone = 100 * time.Millisecond
	p := newOutPump(l.add, lim, func() { close(gone) })
	select {
	case <-gone:
	case <-time.After(3 * time.Second):
		t.Fatal("the reader was never declared gone")
	}
	_, err := p.Write([]byte("x"))
	assert.Error(t, err)
	fs := l.all()
	require.NotEmpty(t, fs)
	assert.True(t, fs[len(fs)-1].Closed)
}
