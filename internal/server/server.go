package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// ContainerAccess is the interface needed from container (avoid direct import for now).
type ContainerAccess interface {
	SQLiteDB() *sql.DB
	DuckDBDB() *sql.DB
}

// EngineAccess is the interface needed from engine.
type EngineAccess interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error)
}

// PageRenderer renders pages to HTML.
type PageRenderer interface {
	RenderPageHTML(pageID string, params map[string]string) (string, error)
}

// Server is the embedded HTTP server for web preview.
type Server struct {
	httpServer *http.Server
	container  ContainerAccess
	engine     EngineAccess
	renderer   PageRenderer
	sse        *SSEBroker
	logger     *slog.Logger
	allowSQL   bool
	addr       string
	listener   net.Listener
}

// Option configures a Server.
type Option func(*Server)

// WithAddress sets the listen address (e.g. ":8080" or "127.0.0.1:0").
func WithAddress(addr string) Option {
	return func(s *Server) {
		s.addr = addr
	}
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) {
		s.logger = l
	}
}

// WithContainer sets the container access interface.
func WithContainer(c ContainerAccess) Option {
	return func(s *Server) {
		s.container = c
	}
}

// WithEngine sets the engine access interface.
func WithEngine(e EngineAccess) Option {
	return func(s *Server) {
		s.engine = e
	}
}

// WithRenderer sets the page renderer.
func WithRenderer(r PageRenderer) Option {
	return func(s *Server) {
		s.renderer = r
	}
}

// WithAllowSQL enables ad-hoc SQL query execution via the API.
func WithAllowSQL(allow bool) Option {
	return func(s *Server) {
		s.allowSQL = allow
	}
}

// New creates a new Server with the given options.
func New(opts ...Option) *Server {
	s := &Server{
		addr:   "127.0.0.1:0",
		logger: slog.Default(),
		sse:    NewSSEBroker(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start starts the HTTP server. It binds the listener and begins serving
// requests in a background goroutine. Use Addr() to get the actual address
// after Start returns.
func (s *Server) Start() error {
	handler := s.setupRoutes()

	s.httpServer = &http.Server{
		Handler: handler,
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}
	s.listener = ln

	s.sse.Start()

	s.logger.Info("server started", "addr", ln.Addr().String())

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping server")
	s.sse.Stop()

	if s.httpServer == nil {
		return nil
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	return nil
}

// Addr returns the address the server is listening on.
// Only valid after Start has been called.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
