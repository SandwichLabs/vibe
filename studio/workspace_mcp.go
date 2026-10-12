package main

// Studio's own MCP switch.

import (
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/hub"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
)

// MCPStatus is Studio's own MCP switch. It never carries the bearer token.
type MCPStatus struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	// CanStart is false when this window cannot serve MCP (read-only, or no
	// workspace); Reason says why.
	CanStart bool   `json:"canStart"`
	Reason   string `json:"reason"`
}

// StartMCP serves this workspace's agent tools over MCP on a loopback port and
// publishes the endpoint in hub.lock, so `vibe mcp serve` (what Claude Desktop
// and Claude Code launch) proxies to Studio. A write that needs approval shows
// the same approval card the chat agent uses.
func (a *App) StartMCP() (*MCPStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if a.readOnly || a.hold == nil {
		return nil, errors.New("This window opened the workspace read-only, so it can't serve MCP. Take over the workspace first.")
	}
	if a.wmcp == nil {
		h := a.newAgentToolHandler(agents.ModeFull)
		h.enableApprovals("mcp")
		m, err := hub.StartWorkspaceMCP(a.ws, a.sess, a.hold,
			hub.WithMCPLogger(a.logger),
			hub.WithMCPRememberPort(),
			hub.WithMCPHandler(h.inner),
			hub.WithMCPHandlerWrap(func(vibemcp.Handler) vibemcp.Handler { return h }),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to start the MCP server: %w", err)
		}
		a.wmcp, a.mcpTool = m, h
		a.logger.Info("MCP server started", "url", m.URL())
	}
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return a.mcpStatusLocked(), nil
}

// StopMCP turns Studio's MCP server off and clears the endpoint from hub.lock.
func (a *App) StopMCP() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wmcp == nil {
		return nil
	}
	err := a.wmcp.Stop()
	a.wmcp, a.mcpTool = nil, nil
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return err
}

// GetMCPStatus reports whether Studio's MCP server is running and where. It
// never includes the bearer token.
func (a *App) GetMCPStatus() *MCPStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mcpStatusLocked()
}

func (a *App) mcpStatusLocked() *MCPStatus {
	st := &MCPStatus{CanStart: a.ws != nil && !a.readOnly && a.hold != nil}
	switch {
	case a.ws == nil:
		st.Reason = "No workspace is open."
	case a.readOnly:
		st.Reason = "This window opened the workspace read-only. Take over the workspace to serve MCP."
	}
	if a.wmcp != nil {
		st.Running, st.URL = true, a.wmcp.URL()
	}
	return st
}

// GetClaudeConfig returns the two snippets "Copy Claude Desktop config" and
// "Copy Claude Code config" put on the clipboard. Neither contains a token: they
// launch `vibe mcp serve --workspace DIR`, which reads the endpoint from
// hub.lock when Studio owns the workspace.
func (a *App) GetClaudeConfig() (*hub.ClaudeConfig, error) {
	a.mu.RLock()
	root := ""
	if a.ws != nil {
		root = a.ws.Root()
	}
	a.mu.RUnlock()
	if root == "" {
		return nil, errNoWorkspace
	}
	locate := a.locateVibe
	if locate == nil {
		locate = hub.LocateVibe
	}
	exe, err := locate()
	if err != nil {
		return nil, errors.New("Couldn't find the `vibe` command-line program. Install it, or put it on your PATH, then try again.")
	}
	cfg, err := hub.BuildClaudeConfig(root, exe)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
