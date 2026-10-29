package repl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Options.Expand rewrites the statement before the editor's gate and the
// engine see it; the history keeps what was typed.
func TestExpandHookRunsBeforeTheGate(t *testing.T) {
	var seen []string
	f := newSession(t, func(o *Options) {
		o.Expand = func(_ context.Context, text, conn string) (string, map[string]any, error) {
			seen = append(seen, text+"|"+conn)
			switch {
			case strings.Contains(text, "answer()"):
				return "SELECT 42 + $extra AS answer", map[string]any{"extra": int64(1)}, nil
			case strings.Contains(text, "boom()"):
				return "", nil, errors.New("metric 'boom' is not here")
			case strings.Contains(text, "evil()"):
				return "INSTALL httpfs", nil, nil // the gate sees the expanded text
			}
			return text, nil, nil
		}
	})
	f.waitFor(prompt)
	f.run("SELECT answer();\r")
	assert.Contains(t, f.out.String(), "43")
	f.run("SELECT boom();\r")
	assert.Contains(t, f.out.String(), "metric 'boom' is not here")
	f.run("SELECT evil();\r")
	assert.Contains(t, f.out.String(), "INSTALL")
	assert.Contains(t, f.out.String(), "Error:")
	f.run("SELECT 1;\r")
	assert.Len(t, seen, 4)
	assert.NotEmpty(t, seen[0])
}
