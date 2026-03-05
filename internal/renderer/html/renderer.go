package html

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// Renderer renders executed page nodes as HTML.
type Renderer struct {
	logger *slog.Logger
}

// NewRenderer creates a new HTML renderer.
func NewRenderer(logger *slog.Logger) *Renderer {
	return &Renderer{
		logger: logger,
	}
}

// RenderedNode holds a single rendered HTML fragment.
type RenderedNode struct {
	HTML string
}

// RenderPage renders a full page to an HTML string.
func (r *Renderer) RenderPage(ctx *renderer.RenderContext, nodes []RenderedNode) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("render context is required")
	}

	var content strings.Builder
	for _, node := range nodes {
		content.WriteString(node.HTML)
		content.WriteString("\n")
	}

	return r.RenderLayout(ctx.PageTitle, nil, content.String())
}
