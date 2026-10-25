package agents

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`

	// StopTurn is set only by the task-mode terminal tools (write_cell,
	// need_more_info) on a successful (non-validation-rejected) call — see
	// handleWriteCell/handleNeedMoreInfo. internal/llm's Task() loop reads it
	// to end the fantasy agent's turn (fantasy.ToolResponse.StopTurn) instead
	// of continuing the tool-calling loop. Every other tool leaves this
	// false/omitted; JSON-RPC callers (MCP, a raw request) can ignore it.
	StopTurn bool `json:"stopTurn,omitempty"`
}

// Error is a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard error codes.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	ErrPermission     = -32000
	ErrApprovalNeeded = -32001
)

// NewResponse creates a successful JSON-RPC 2.0 response.
func NewResponse(id any, result any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  emptyListsNotNull(result),
	}
}

// emptyListsNotNull replaces every nil slice that is a value of a result map
// (recursing into nested maps and lists of maps) with an empty slice of the
// same type, so a tool result says [] for "none" and never null. Byte slices
// are left alone. It does not look inside [][]any or []any: those hold SQL
// result rows and cells, where a NULL list value is data and must stay null
// (only a nil rows slice itself becomes []). Results that are not maps pass
// through unchanged.
func emptyListsNotNull(result any) any {
	v := reflect.ValueOf(result)
	if v.IsValid() && v.Kind() == reflect.Slice && v.IsNil() && v.Type().Elem().Kind() != reflect.Uint8 {
		return reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	switch m := result.(type) {
	case map[string]any:
		for k, e := range m {
			m[k] = emptyListsNotNull(e)
		}
	case []map[string]any:
		for _, e := range m {
			emptyListsNotNull(e)
		}
	}
	return result
}

// NewErrorResponse creates an error JSON-RPC 2.0 response.
func NewErrorResponse(id any, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code: code,
			// Agent- and MCP-facing: connection errors echo user='...', which the
			// agent never needs (the UI shows it to the person, not through here).
			Message: secrets.RedactIdentity(message),
		},
	}
}

// ParseRequest parses a JSON-RPC 2.0 request from raw bytes.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC request: %w", err)
	}
	if req.JSONRPC != "2.0" {
		return nil, fmt.Errorf("invalid JSON-RPC version: %q", req.JSONRPC)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("missing method in JSON-RPC request")
	}
	return &req, nil
}

// Marshal serializes the response to JSON bytes.
func (r *Response) Marshal() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC response: %w", err)
	}
	return data, nil
}
