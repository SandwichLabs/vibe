package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// Transport defines the interface for reading requests and writing responses.
type Transport interface {
	ReadRequest() (*Request, error)
	WriteResponse(resp *Response) error
	Close() error
}

// StdioTransport implements Transport over stdin/stdout using newline-delimited JSON.
type StdioTransport struct {
	reader *bufio.Scanner
	writer io.Writer
	mu     sync.Mutex
}

// NewStdioTransport creates a new stdio-based transport.
func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &StdioTransport{
		reader: scanner,
		writer: w,
	}
}

// ReadRequest reads a single JSON-RPC request from stdin.
func (t *StdioTransport) ReadRequest() (*Request, error) {
	if !t.reader.Scan() {
		if err := t.reader.Err(); err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		return nil, io.EOF
	}
	return ParseRequest(t.reader.Bytes())
}

// WriteResponse writes a JSON-RPC response to stdout.
func (t *StdioTransport) WriteResponse(resp *Response) error {
	data, err := resp.Marshal()
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = fmt.Fprintf(t.writer, "%s\n", data)
	return err
}

// Close is a no-op for stdio transport.
func (t *StdioTransport) Close() error {
	return nil
}

// SocketTransport implements Transport over a TCP socket using newline-delimited JSON.
type SocketTransport struct {
	listener net.Listener
	conns    []net.Conn
	mu       sync.Mutex
	active   net.Conn
	scanner  *bufio.Scanner
}

// NewSocketTransport creates a new socket-based transport listening on the given address.
func NewSocketTransport(addr string) (*SocketTransport, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	return &SocketTransport{
		listener: listener,
	}, nil
}

// ReadRequest accepts a connection (if needed) and reads a request.
func (t *SocketTransport) ReadRequest() (*Request, error) {
	if t.active == nil {
		conn, err := t.listener.Accept()
		if err != nil {
			return nil, fmt.Errorf("failed to accept connection: %w", err)
		}
		t.mu.Lock()
		t.active = conn
		t.conns = append(t.conns, conn)
		t.scanner = bufio.NewScanner(conn)
		t.scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		t.mu.Unlock()
	}

	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read from socket: %w", err)
		}
		// Connection closed, reset and wait for a new one.
		t.mu.Lock()
		t.active = nil
		t.scanner = nil
		t.mu.Unlock()
		return nil, io.EOF
	}
	return ParseRequest(t.scanner.Bytes())
}

// WriteResponse writes a JSON-RPC response to the active connection.
func (t *SocketTransport) WriteResponse(resp *Response) error {
	t.mu.Lock()
	conn := t.active
	t.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no active connection")
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	_, err = fmt.Fprintf(conn, "%s\n", data)
	return err
}

// Close closes the listener and all connections.
func (t *SocketTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, conn := range t.conns {
		conn.Close()
	}
	t.conns = nil
	t.active = nil
	return t.listener.Close()
}

// AgentServer manages a transport and dispatches requests to a handler.
type AgentServer struct {
	handler   *Handler
	transport Transport
	logger    *slog.Logger
}

// NewAgentServer creates a new agent server.
func NewAgentServer(handler *Handler, transport Transport, logger *slog.Logger) *AgentServer {
	return &AgentServer{
		handler:   handler,
		transport: transport,
		logger:    logger,
	}
}

// Serve reads requests in a loop and dispatches them to the handler.
func (s *AgentServer) Serve(ctx context.Context) error {
	s.logger.Info("agent server started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("agent server shutting down")
			return ctx.Err()
		default:
		}

		req, err := s.transport.ReadRequest()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.logger.Info("transport closed")
				return nil
			}
			s.logger.Error("failed to read request", "error", err)
			resp := NewErrorResponse(nil, ErrParse, err.Error())
			if writeErr := s.transport.WriteResponse(resp); writeErr != nil {
				s.logger.Error("failed to write error response", "error", writeErr)
			}
			continue
		}

		resp := s.handler.Handle(ctx, req)
		if err := s.transport.WriteResponse(resp); err != nil {
			s.logger.Error("failed to write response", "error", err)
		}
	}
}

// Stop closes the transport.
func (s *AgentServer) Stop() error {
	return s.transport.Close()
}
