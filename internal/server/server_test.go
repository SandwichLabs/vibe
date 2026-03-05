package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalhostOnly_AllowsLoopback(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})

	handler := localhostOnly(inner)

	// Request from 127.0.0.1 should be allowed.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestLocalhostOnly_AllowsIPv6Loopback(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := localhostOnly(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[::1]:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLocalhostOnly_BlocksNonLocalhost(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := localhostOnly(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLocalhostOnly_BlocksInvalidAddr(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := localhostOnly(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-an-address"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSSEBroker_BroadcastEvents(t *testing.T) {
	broker := NewSSEBroker()
	broker.Start()
	defer broker.Stop()

	// Register a client channel.
	ch := make(chan string, 16)
	broker.register <- ch

	// Give the broker goroutine time to process registration.
	time.Sleep(10 * time.Millisecond)

	broker.Broadcast("reload")

	select {
	case event := <-ch:
		assert.Equal(t, "reload", event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}

func TestSSEBroker_MultipleClients(t *testing.T) {
	broker := NewSSEBroker()
	broker.Start()
	defer broker.Stop()

	ch1 := make(chan string, 16)
	ch2 := make(chan string, 16)
	broker.register <- ch1
	broker.register <- ch2

	time.Sleep(10 * time.Millisecond)

	broker.Broadcast("update")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case event := <-ch1:
			assert.Equal(t, "update", event)
		case <-time.After(time.Second):
			t.Error("ch1 timed out")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case event := <-ch2:
			assert.Equal(t, "update", event)
		case <-time.After(time.Second):
			t.Error("ch2 timed out")
		}
	}()

	wg.Wait()
}

func TestSSEBroker_ServeHTTP(t *testing.T) {
	broker := NewSSEBroker()
	broker.Start()
	defer broker.Stop()

	// Use a test server to test SSE streaming.
	ts := httptest.NewServer(broker)
	defer ts.Close()

	// Send a broadcast after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Broadcast("test-event")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Read enough to see initial "connected" and the broadcast.
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	assert.Contains(t, body, "data: connected")

	// Read the broadcast event.
	n, _ = resp.Body.Read(buf)
	body = string(buf[:n])
	assert.Contains(t, body, "data: test-event")
}

func TestSetupRoutes_DoesNotPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(WithLogger(logger))

	assert.NotPanics(t, func() {
		handler := s.setupRoutes()
		assert.NotNil(t, handler)
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	handler := recovery(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoggingMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	handler := logging(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"simple name", "users", true},
		{"with underscore", "user_data", true},
		{"with numbers", "data2024", true},
		{"starts with number", "2data", false},
		{"empty", "", false},
		{"has space", "user data", false},
		{"has dash", "user-data", false},
		{"sql injection", "users; DROP TABLE", false},
		{"vibe prefix", "_vibe_meta", false},
		{"vibe prefix other", "_vibe_pages", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidIdentifier(tt.input))
		})
	}
}

func TestHandleQuery_DisabledByDefault(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(WithLogger(logger))

	req := httptest.NewRequest(http.MethodPost, "/api/query",
		strings.NewReader(`{"sql":"SELECT 1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleQuery(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "disabled")
}

func TestServerStartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(
		WithAddress("127.0.0.1:0"),
		WithLogger(logger),
	)

	err := s.Start()
	require.NoError(t, err)

	addr := s.Addr()
	assert.NotEmpty(t, addr)

	// Verify server is responding.
	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	resp.Body.Close()

	// Stop the server.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = s.Stop(ctx)
	assert.NoError(t, err)
}

func TestServerAddr_BeforeStart(t *testing.T) {
	s := New()
	assert.Empty(t, s.Addr())
}
