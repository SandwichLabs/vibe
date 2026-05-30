package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sandwich-labs/vibe-app/internal/agents"
)

// Server wraps an MCP server with SSE transport, bridging to the agents.Handler.
type Server struct {
	handler    *agents.Handler
	mcpServer  *server.MCPServer
	httpServer *http.Server
	listener   net.Listener
	logger     *slog.Logger
}

// New creates a new MCP server backed by the given agents handler.
func New(handler *agents.Handler, logger *slog.Logger) *Server {
	return &Server{
		handler: handler,
		logger:  logger,
	}
}

// Start creates the MCP server, registers tools, and starts the HTTP/SSE listener.
func (s *Server) Start(addr string) error {
	s.mcpServer = server.NewMCPServer(
		"Vibe Studio",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	for _, tool := range agents.ListTools() {
		mcpTool := toolDefToMCPTool(tool)
		toolName := tool.Name
		s.mcpServer.AddTool(mcpTool, s.makeToolHandler(toolName))
	}

	sseServer := server.NewSSEServer(s.mcpServer)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	httpServer := &http.Server{
		Handler: sseServer,
	}
	s.httpServer = httpServer

	s.logger.Info("MCP server starting", "addr", ln.Addr().String())

	// Capture httpServer/ln as locals so the serve goroutine never reads the
	// struct fields that Stop() nils out concurrently (avoids a nil-pointer race
	// when Start and Stop happen in quick succession, e.g. an MCP restart on a
	// live mode flip).
	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("MCP server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	if s.httpServer == nil {
		return nil
	}
	s.logger.Info("MCP server stopping")
	err := s.httpServer.Shutdown(context.Background())
	s.httpServer = nil
	s.listener = nil
	s.mcpServer = nil
	return err
}

// Addr returns the bound listener address, or empty string if not running.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// makeToolHandler returns an MCP tool handler that dispatches to the agents handler.
func (s *Server) makeToolHandler(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := make(map[string]any)
		if args := request.GetArguments(); args != nil {
			for k, v := range args {
				params[k] = v
			}
		}

		req := &agents.Request{
			JSONRPC: "2.0",
			ID:      request.Params.Name,
			Method:  toolName,
			Params:  params,
		}

		resp := s.handler.Handle(ctx, req)

		if resp.Error != nil {
			return mcp.NewToolResultError(resp.Error.Message), nil
		}

		data, err := json.Marshal(resp.Result)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// toolDefToMCPTool converts an agents.ToolDef to an mcp.Tool.
func toolDefToMCPTool(td agents.ToolDef) mcp.Tool {
	opts := []mcp.ToolOption{
		mcp.WithDescription(td.Description),
	}

	if len(td.Params) > 0 {
		properties := make(map[string]any)
		var required []string

		for _, p := range td.Params {
			prop := map[string]any{
				"type":        paramTypeToJSONSchema(p.Type),
				"description": p.Desc,
			}
			properties[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}

		opts = append(opts, func(t *mcp.Tool) {
			if t.InputSchema.Properties == nil {
				t.InputSchema.Properties = make(map[string]any)
			}
			for k, v := range properties {
				t.InputSchema.Properties[k] = v
			}
			t.InputSchema.Required = required
		})
	}

	return mcp.NewTool(td.Name, opts...)
}

func paramTypeToJSONSchema(t string) string {
	switch t {
	case "string":
		return "string"
	case "boolean":
		return "boolean"
	case "object":
		return "object"
	case "array":
		return "array"
	default:
		return "string"
	}
}
