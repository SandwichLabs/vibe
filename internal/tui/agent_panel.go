package tui

import (
	"fmt"
	"strings"
	"time"
)

// AgentStatus represents the agent connection state.
type AgentStatus int

const (
	AgentDisconnected AgentStatus = iota
	AgentConnecting
	AgentConnected
	AgentError
)

// ToolCallEntry records a single agent tool call for display.
type ToolCallEntry struct {
	Timestamp time.Time
	Method    string
	Status    string
}

// AgentPanelModel manages the agent connection status and tool call log.
type AgentPanelModel struct {
	status   AgentStatus
	errMsg   string
	toolLog  []ToolCallEntry
	scrollY  int
	visible  bool
}

// NewAgentPanelModel creates a new agent panel.
func NewAgentPanelModel() AgentPanelModel {
	return AgentPanelModel{
		status:  AgentDisconnected,
		toolLog: make([]ToolCallEntry, 0),
	}
}

// IsVisible returns whether the agent panel is showing.
func (a AgentPanelModel) IsVisible() bool {
	return a.visible
}

// Toggle toggles the agent panel visibility.
func (a *AgentPanelModel) Toggle() {
	a.visible = !a.visible
}

// SetStatus updates the agent connection status.
func (a *AgentPanelModel) SetStatus(status AgentStatus, errMsg string) {
	a.status = status
	a.errMsg = errMsg
}

// AddToolCall appends a tool call entry to the log.
func (a *AgentPanelModel) AddToolCall(method, status string) {
	a.toolLog = append(a.toolLog, ToolCallEntry{
		Timestamp: time.Now(),
		Method:    method,
		Status:    status,
	})
}

// View renders the agent panel within the given dimensions.
func (a AgentPanelModel) View(width, height int) string {
	if !a.visible {
		return ""
	}

	var b strings.Builder

	title := contentTitleStyle.Render("Agent Panel")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Connection status
	b.WriteString(sectionHeaderStyle.Render("Status: "))
	switch a.status {
	case AgentDisconnected:
		b.WriteString(dimStyle.Render("Disconnected"))
	case AgentConnecting:
		b.WriteString(warningStyle.Render("Connecting..."))
	case AgentConnected:
		b.WriteString(successStyle.Render("Connected"))
	case AgentError:
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s", a.errMsg)))
	}
	b.WriteString("\n\n")

	// Tool call log
	b.WriteString(sectionHeaderStyle.Render("Tool Call Log:"))
	b.WriteString("\n")

	if len(a.toolLog) == 0 {
		b.WriteString(dimStyle.Render("  (no tool calls)"))
		b.WriteString("\n")
	} else {
		start := 0
		if len(a.toolLog) > height-8 {
			start = len(a.toolLog) - (height - 8)
		}
		for _, entry := range a.toolLog[start:] {
			ts := entry.Timestamp.Format("15:04:05")
			line := fmt.Sprintf("  %s  %-20s  %s", ts, entry.Method, entry.Status)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// warningStyle is used for warning text in the agent panel.
var warningStyle = warningColorStyle

// warningColorStyle provides an orange-colored text style.
var warningColorStyle = dimStyle.Foreground(warningColor)
