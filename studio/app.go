package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	vibemcp "github.com/sandwich-labs/vibe-app/internal/mcp"
	"github.com/sandwich-labs/vibe-app/internal/pages"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	htmlrenderer "github.com/sandwich-labs/vibe-app/internal/renderer/html"
	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yuin/goldmark"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// engineAdapter wraps engine.Manager to satisfy pages.QueryEngine interface.
type engineAdapter struct {
	mgr *engine.Manager
}

func (ea *engineAdapter) Execute(ctx context.Context, sql string, params map[string]any) (*pages.ResultSet, error) {
	rs, err := ea.mgr.Execute(ctx, sql, params)
	if err != nil {
		return nil, err
	}
	prs := &pages.ResultSet{Rows: rs.Rows}
	for _, col := range rs.Columns {
		prs.Columns = append(prs.Columns, pages.Column{Name: col.Name, Type: col.Type})
	}
	return prs, nil
}

// App is the main application struct bound to the Wails frontend.
type App struct {
	ctx         context.Context
	logger      *slog.Logger
	mu          sync.RWMutex
	container   *container.Container
	engine      *engine.Manager
	registry    *queries.Registry
	renderer    *htmlrenderer.Renderer
	filePath    string
	mcpServer   *vibemcp.Server
	initialFile string

	renderMu     sync.Mutex
	renderCancel context.CancelFunc
}

// NewApp creates a new App instance.
func NewApp(logger *slog.Logger) *App {
	return &App{
		logger:   logger,
		renderer: htmlrenderer.NewRenderer(logger),
	}
}

// GetInitialFile returns the file path passed as a command-line argument, if any.
func (a *App) GetInitialFile() string {
	return a.initialFile
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(_ context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mcpServer != nil {
		a.mcpServer.Stop() //nolint:errcheck
		a.mcpServer = nil
	}
	if a.container != nil {
		a.container.Save()  //nolint:errcheck
		a.container.Close() //nolint:errcheck
		a.container = nil
	}
}

// --- File Operations ---

// SelectVibeFile opens a file dialog to select a .vibe file.
func (a *App) SelectVibeFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open Vibe File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Files (*.vibe)", Pattern: "*.vibe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// SelectNewVibeFile opens a save dialog to create a new .vibe file.
func (a *App) SelectNewVibeFile() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Create Vibe File",
		DefaultFilename: "untitled.vibe",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Files (*.vibe)", Pattern: "*.vibe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	return path, nil
}

// OpenFile opens an existing .vibe file with the given passphrase.
func (a *App) OpenFile(path, passphrase string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container != nil {
		a.container.Save()  //nolint:errcheck
		a.container.Close() //nolint:errcheck
	}

	c, err := container.Open(path,
		container.WithPassphrase(passphrase),
		container.WithLogger(a.logger),
	)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	a.container = c
	a.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(a.logger))
	a.registry = queries.NewRegistry(c.SQLiteDB(), a.logger)
	a.filePath = path
	runtime.EventsEmit(a.ctx, "file:opened")
	return nil
}

// CreateFile creates a new .vibe file.
func (a *App) CreateFile(path, passphrase, title, desc string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container != nil {
		a.container.Save()  //nolint:errcheck
		a.container.Close() //nolint:errcheck
	}

	c, err := container.Create(path,
		container.WithPassphrase(passphrase),
		container.WithLogger(a.logger),
		container.WithTitle(title),
		container.WithDescription(desc),
	)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	a.container = c
	a.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(a.logger))
	a.registry = queries.NewRegistry(c.SQLiteDB(), a.logger)
	a.filePath = path
	runtime.EventsEmit(a.ctx, "file:opened")
	return nil
}

// CloseFile saves and closes the current file.
func (a *App) CloseFile() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil
	}

	if a.mcpServer != nil {
		a.mcpServer.Stop() //nolint:errcheck
		a.mcpServer = nil
	}

	if err := a.container.Save(); err != nil {
		return fmt.Errorf("failed to save: %w", err)
	}
	if err := a.container.Close(); err != nil {
		return fmt.Errorf("failed to close: %w", err)
	}
	a.container = nil
	a.engine = nil
	a.registry = nil
	a.filePath = ""
	runtime.EventsEmit(a.ctx, "file:closed")
	return nil
}

// SaveFile saves the current file.
func (a *App) SaveFile() error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	return a.container.Save()
}

// IsFileOpen returns whether a file is currently open.
func (a *App) IsFileOpen() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.container != nil
}

// FileInfo holds metadata about the open file.
type FileInfo struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PageCount   int    `json:"page_count"`
	TableCount  int    `json:"table_count"`
	QueryCount  int    `json:"query_count"`
	FileSize    int64  `json:"file_size"`
}

// GetFileInfo returns metadata about the currently open file.
func (a *App) GetFileInfo() (*FileInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	info := &FileInfo{Path: a.filePath}

	title, err := a.container.Meta("title")
	if err == nil {
		info.Title = title
	}
	desc, err := a.container.Meta("description")
	if err == nil {
		info.Description = desc
	}

	db := a.container.SQLiteDB()
	db.QueryRow("SELECT COUNT(*) FROM _vibe_pages").Scan(&info.PageCount)    //nolint:errcheck
	db.QueryRow("SELECT COUNT(*) FROM _vibe_queries").Scan(&info.QueryCount) //nolint:errcheck

	if duckDB := a.container.DuckDBDB(); duckDB != nil {
		duckDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main'").Scan(&info.TableCount) //nolint:errcheck
	}

	if fi, err := os.Stat(a.filePath); err == nil {
		info.FileSize = fi.Size()
	}

	return info, nil
}

// --- Pages ---

// PageSummary holds a page list entry.
type PageSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	PageType  string `json:"page_type"`
	SortOrder int    `json:"sort_order"`
}

// ListPages returns all pages in the container.
func (a *App) ListPages() ([]PageSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	// System pages (id prefixed with '_', e.g. the `_dashboard` page that backs
	// the Studio Home) are excluded from the user-facing Pages list (P3.10).
	rows, err := db.Query("SELECT id, title, page_type, sort_order FROM _vibe_pages WHERE id NOT LIKE '\\_%' ESCAPE '\\' ORDER BY sort_order, title")
	if err != nil {
		return nil, fmt.Errorf("failed to list pages: %w", err)
	}
	defer rows.Close()

	result := make([]PageSummary, 0)
	for rows.Next() {
		var p PageSummary
		if err := rows.Scan(&p.ID, &p.Title, &p.PageType, &p.SortOrder); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// PageDetail holds full page content.
type PageDetail struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	PageType  string `json:"page_type"`
	SortOrder int    `json:"sort_order"`
}

// GetPage returns a single page by ID.
func (a *App) GetPage(id string) (*PageDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	var p PageDetail
	err := db.QueryRow("SELECT id, title, content, page_type, sort_order FROM _vibe_pages WHERE id = ?", id).
		Scan(&p.ID, &p.Title, &p.Content, &p.PageType, &p.SortOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}
	return &p, nil
}

// CancelRender cancels any in-flight page render, killing running DB queries.
func (a *App) CancelRender() {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	if a.renderCancel != nil {
		a.renderCancel()
		a.renderCancel = nil
	}
}

// RenderPageContent renders a page's content as HTML fragments.
func (a *App) RenderPageContent(id string, params map[string]string) (string, error) {
	// Cancel any previous render and create a new cancellable context.
	a.renderMu.Lock()
	if a.renderCancel != nil {
		a.renderCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.renderCancel = cancel
	a.renderMu.Unlock()

	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	var content, pageType string
	err := db.QueryRow("SELECT content, page_type FROM _vibe_pages WHERE id = ?", id).Scan(&content, &pageType)
	if err != nil {
		return "", fmt.Errorf("failed to get page content: %w", err)
	}

	page, err := pages.Parse(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse page: %w", err)
	}

	// Convert string params to any params for engine.
	engineParams := make(map[string]any, len(params))
	for k, v := range params {
		engineParams[k] = v
	}

	executed, err := pages.Execute(ctx, page, &engineAdapter{mgr: a.engine}, engineParams)
	if err != nil {
		return "", fmt.Errorf("failed to execute page: %w", err)
	}

	// Expand Go template syntax in text nodes.
	executed, err = pages.ExpandTemplates(executed, engineParams)
	if err != nil {
		return "", fmt.Errorf("failed to expand templates: %w", err)
	}

	return a.renderExecutedNodes(executed, pageType)
}

// renderExecutedNodes renders executed page nodes to HTML fragments. The page's
// resolved variable registry (frontmatter + params) is threaded down so input
// filters (P3.6) can mark their current selection as selected/checked.
func (a *App) renderExecutedNodes(executed *pages.ExecutedPage, pageType string) (string, error) {
	return a.renderNodeList(executed.Nodes, pageType, executed.Variables)
}

// renderNodeList renders a slice of executed nodes to HTML.
func (a *App) renderNodeList(nodes []pages.ExecutedNode, pageType string, vars map[string]any) (string, error) {
	var b strings.Builder

	for _, en := range nodes {
		switch n := en.Node.(type) {
		case *pages.TextNode:
			var rendered string
			if pageType == "html" {
				rendered = n.Content
			} else {
				var mdBuf bytes.Buffer
				if err := goldmark.Convert([]byte(n.Content), &mdBuf); err != nil {
					rendered = "<div class=\"prose\">" + n.Content + "</div>\n"
				} else {
					rendered = "<div class=\"prose\">" + mdBuf.String() + "</div>\n"
				}
			}
			// Substitute resolved inline {value:} directive placeholders with
			// formatted inline <span> elements (post-markdown so they survive
			// goldmark untouched and can embed within prose).
			rendered = substituteInlineValues(rendered, en.InlineValues)
			b.WriteString(rendered)

		case *pages.SQLBlockNode:
			// SQL blocks don't render directly (they produce result sets for directives).

		case *pages.DirectiveNode:
			html, err := a.renderDirective(n, en.Result, en.InlineValues, vars)
			if err != nil {
				b.WriteString(fmt.Sprintf("<div class=\"error\">Error rendering %s: %s</div>\n", n.Type, err))
				continue
			}
			b.WriteString(html)
			b.WriteString("\n")

		case *pages.RowNode:
			html, err := a.renderRow(n, en.Children, pageType, vars)
			if err != nil {
				b.WriteString(fmt.Sprintf("<div class=\"error\">Error rendering row: %s</div>\n", err))
				continue
			}
			b.WriteString(html)
			b.WriteString("\n")

		case *pages.BlockNode:
			html, err := a.renderBlock(n, en.Children, pageType, vars)
			if err != nil {
				b.WriteString(fmt.Sprintf("<div class=\"error\">Error rendering %s: %s</div>\n", n.Kind, err))
				continue
			}
			b.WriteString(html)
			b.WriteString("\n")

		case *pages.ConditionalNode:
			// Defensive: pages.Execute resolves conditionals by inlining the
			// surviving branch's children, so a ConditionalNode should never
			// reach here. If one does (e.g. a future code path that skips the
			// executor), render its already-executed children rather than
			// silently swallowing the node.
			html, err := a.renderNodeList(en.Children, pageType, vars)
			if err != nil {
				b.WriteString(fmt.Sprintf("<div class=\"error\">Error rendering conditional: %s</div>\n", err))
				continue
			}
			b.WriteString(html)

		default:
			// Unknown node types are dropped intentionally; SQLBlockNode is the
			// only other expected case and renders nothing on its own.
		}
	}

	return b.String(), nil
}

// renderRow renders a RowNode as a CSS grid container. Each direct child
// becomes a grid cell. The "cols" config sets the column count (default 2).
func (a *App) renderRow(node *pages.RowNode, children []pages.ExecutedNode, pageType string, vars map[string]any) (string, error) {
	cols := "2"
	if c, ok := node.Config["cols"]; ok {
		if s, ok := c.(string); ok && s != "" {
			cols = s
		}
	}
	gap := "1rem"
	if g, ok := node.Config["gap"]; ok {
		if s, ok := g.(string); ok && s != "" {
			gap = s
		}
	}

	// Render each child as a separate grid cell.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<div class=\"vibe-row\" style=\"grid-template-columns: repeat(%s, 1fr); gap: %s;\">", cols, gap))

	for _, en := range children {
		cellHTML, err := a.renderNodeList([]pages.ExecutedNode{en}, pageType, vars)
		if err != nil {
			return "", err
		}
		b.WriteString("<div class=\"vibe-col\">")
		b.WriteString(cellHTML)
		b.WriteString("</div>")
	}

	b.WriteString("</div>")
	return b.String(), nil
}

// renderBlock renders a generalized layout BlockNode (P3.5): {stack}, {tabs}
// (with {tab:} sections), and {callout}. Each block wraps its already-executed
// children, recursing through renderNodeList so nested directives/SQL resolve.
func (a *App) renderBlock(node *pages.BlockNode, children []pages.ExecutedNode, pageType string, vars map[string]any) (string, error) {
	switch node.Kind {
	case "tabs":
		return a.renderTabs(node, children, pageType, vars)
	case "callout":
		return a.renderCallout(node, children, pageType, vars)
	default: // "stack" and any future vertical container.
		inner, err := a.renderNodeList(children, pageType, vars)
		if err != nil {
			return "", err
		}
		return "<div class=\"vibe-stack\">" + inner + "</div>", nil
	}
}

// renderTabs renders a {tabs} block as a tab header bar plus one switchable
// panel per {tab:} section. Each "tab" child BlockNode contributes a header
// (its label) and a corresponding vibe-tab-panel wrapping its own children.
func (a *App) renderTabs(node *pages.BlockNode, children []pages.ExecutedNode, pageType string, vars map[string]any) (string, error) {
	var headers strings.Builder
	var panels strings.Builder

	tabIdx := 0
	for _, en := range children {
		tab, ok := en.Node.(*pages.BlockNode)
		if !ok || tab.Kind != "tab" {
			// Stray non-tab content inside {tabs} renders before the panels.
			stray, err := a.renderNodeList([]pages.ExecutedNode{en}, pageType, vars)
			if err != nil {
				return "", err
			}
			panels.WriteString(stray)
			continue
		}

		label, _ := tab.Config["tab"].(string)
		active := ""
		if tabIdx == 0 {
			active = " active"
		}
		headers.WriteString(fmt.Sprintf(
			"<button type=\"button\" class=\"vibe-tab-header%s\" data-tab-index=\"%d\">%s</button>",
			active, tabIdx, html.EscapeString(label)))

		panelInner, err := a.renderNodeList(en.Children, pageType, vars)
		if err != nil {
			return "", err
		}
		panels.WriteString(fmt.Sprintf(
			"<div class=\"vibe-tab-panel%s\" data-tab-index=\"%d\">%s</div>",
			active, tabIdx, panelInner))
		tabIdx++
	}

	var b strings.Builder
	b.WriteString("<div class=\"vibe-tabs\">")
	b.WriteString("<div class=\"vibe-tab-headers\">")
	b.WriteString(headers.String())
	b.WriteString("</div>")
	b.WriteString(panels.String())
	b.WriteString("</div>")
	return b.String(), nil
}

// renderCallout renders a {callout: type: warning} block as a typed container
// wrapping its prose/children. The type defaults to "info".
func (a *App) renderCallout(node *pages.BlockNode, children []pages.ExecutedNode, pageType string, vars map[string]any) (string, error) {
	calloutType := "info"
	if t, ok := node.Config["type"].(string); ok && t != "" {
		calloutType = t
	}
	inner, err := a.renderNodeList(children, pageType, vars)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<div class=\"vibe-callout vibe-callout-%s\">%s</div>",
		html.EscapeString(calloutType), inner), nil
}

// renderDirective renders a single directive node to HTML. For a standalone
// {value:} directive, the scalar is pre-resolved by the executor and arrives in
// inlineValues; all other directive types ignore it.
func (a *App) renderDirective(node *pages.DirectiveNode, result *pages.ResultSet, inlineValues []pages.InlineValue, vars map[string]any) (string, error) {
	// Convert pages.ResultSet to renderer.ResultSet.
	var rrs *renderer.ResultSet
	if result != nil {
		rrs = &renderer.ResultSet{
			Rows: result.Rows,
		}
		for _, col := range result.Columns {
			rrs.Columns = append(rrs.Columns, renderer.Column{
				Name: col.Name,
				Type: col.Type,
			})
		}
	}

	switch node.Type {
	case "table":
		return a.renderer.RenderTable(&renderer.TableConfig{
			Source:     getStringConfig(node.Config, "source"),
			Title:      getStringConfig(node.Config, "title"),
			Subtitle:   getStringConfig(node.Config, "subtitle"),
			PageSize:   getIntConfig(node.Config, "page_size"),
			RowShading: getBoolConfig(node.Config, "row_shading"),
			Sortable:   true,
			ColumnFmt:  getStringMapConfig(node.Config, "fmt"),
			Result:     rrs,
		})

	case "chart":
		// {chart: y: ...} accepts either a single column ("revenue") or a list
		// ("[revenue, cost]"). A list produces one dataset per column.
		ySingle, yList := getStringOrSliceConfig(node.Config, "y")

		// `stacked` accepts a bool or a string; the "100%" normalized-stacking
		// mode must survive verbatim. stackMode is "", "true", or "100%".
		stackMode := getStackMode(node.Config, "stacked")

		// `type: horizontal_bar` maps to a Chart.js bar chart with indexAxis "y".
		chartType := getStringConfig(node.Config, "type")
		horizontal := false
		if strings.EqualFold(chartType, "horizontal_bar") {
			chartType = "bar"
			horizontal = true
		}

		return a.renderer.RenderChart(&renderer.ChartConfig{
			Source:       getStringConfig(node.Config, "source"),
			Type:         chartType,
			X:            getStringConfig(node.Config, "x"),
			Y:            ySingle,
			YColumns:     yList,
			Series:       getStringConfig(node.Config, "series"),
			Stacked:      stackMode != "",
			StackMode:    stackMode,
			XFmt:         getStringConfig(node.Config, "x_fmt"),
			YFmt:         getStringConfig(node.Config, "y_fmt"),
			XTitle:       getStringConfig(node.Config, "x_title"),
			YTitle:       getStringConfig(node.Config, "y_title"),
			YRange:       getFloatRangeConfig(node.Config, "y_range"),
			Colors:       getStringSliceConfig(node.Config, "colors"),
			SeriesColors: getStringMapConfig(node.Config, "series_colors"),
			Legend:       getBoolPtrConfig(node.Config, "legend"),
			DataLabels:   getBoolConfig(node.Config, "data_labels"),
			RefLines:     getRefLinesConfig(node.Config, "ref_lines"),
			Horizontal:   horizontal,
			Title:        getStringConfig(node.Config, "title"),
			Color:        getStringConfig(node.Config, "color"),
			Result:       rrs,
		})

	case "metric":
		var value any
		if rrs != nil && len(rrs.Rows) > 0 && len(rrs.Rows[0]) > 0 {
			value = rrs.Rows[0][0]
		}
		// Support both "fmt" key and legacy "format" key
		fmtCode := getStringConfig(node.Config, "fmt")
		cfg := &renderer.MetricConfig{
			Label:  getStringConfig(node.Config, "label"),
			Title:  getStringConfig(node.Config, "title"),
			Format: getStringConfig(node.Config, "format"),
			Fmt:    fmtCode,
			Value:  value,
		}

		// Comparison delta: resolve the prior baseline. Prefer an explicit "prior"
		// column in the first row; otherwise the second column of the first row.
		if comparison := getStringConfig(node.Config, "comparison"); comparison != "" && rrs != nil {
			cfg.Comparison = comparison
			cfg.PriorValue = metricPriorValue(rrs)
		}

		// Sparkline: build a numeric series from the result. For a two-column
		// time-series, the second (numeric) column is the series.
		if getBoolConfig(node.Config, "sparkline") && rrs != nil {
			cfg.Sparkline = true
			cfg.Series = metricSeries(rrs)
			// For a time-series the "current" value is the latest point.
			if len(cfg.Series) > 0 {
				cfg.Value = cfg.Series[len(cfg.Series)-1]
			}
		}

		return a.renderer.RenderMetric(cfg)

	case "detail":
		return a.renderer.RenderDetail(&renderer.DetailConfig{
			Source: getStringConfig(node.Config, "source"),
			Layout: getStringConfig(node.Config, "layout"),
			Result: rrs,
		})

	case "form":
		return a.renderer.RenderForm(&renderer.FormConfig{
			Table: getStringConfig(node.Config, "table"),
		})

	case "value":
		// A standalone-line {value:} directive: the executor pre-resolved the
		// scalar (source+column or inline sql) into inlineValues. Render it as an
		// inline <span> mirroring the inline-prose path, with a nil value (missing
		// column / empty result) degrading to a safe em-dash placeholder rather
		// than crashing.
		var iv pages.InlineValue
		if len(inlineValues) > 0 {
			iv = inlineValues[0]
		}
		var text string
		if iv.Value == nil {
			text = "—" // em-dash placeholder for missing/empty values
		} else if iv.Fmt != "" {
			text = renderer.Format(iv.Value, iv.Fmt)
		} else {
			text = fmt.Sprintf("%v", iv.Value)
		}
		return "<span class=\"vibe-value\">" + html.EscapeString(text) + "</span>", nil

	case "dropdown", "button_group", "slider", "text_input", "toggle":
		// Interactive input filters (P3.6). Each registers a named variable; its
		// current value (from the page params/frontmatter threaded down in vars)
		// is rendered as the control's selection so it persists across the full
		// re-render (Option A reactivity).
		return a.renderInput(node, rrs, vars), nil

	default:
		return fmt.Sprintf("<div>Unknown directive: %s</div>", node.Type), nil
	}
}

// renderInput renders an interactive input-filter directive (P3.6) to an HTML
// control wired for full-page-re-render reactivity (Option A). Each control
// registers the variable named by the directive's primary value (`name`) and
// emits a `data-vibe-input` attribute carrying that name so the frontend can
// collect the new value and re-invoke RenderPageContent with it. The control's
// current value comes from the threaded variable registry so the selection
// persists after re-render.
func (a *App) renderInput(node *pages.DirectiveNode, rrs *renderer.ResultSet, vars map[string]any) string {
	name := getStringConfig(node.Config, "name")
	title := getStringConfig(node.Config, "title")
	current := ""
	if vars != nil {
		if v, ok := vars[name]; ok && v != nil {
			current = fmt.Sprintf("%v", v)
		}
	}

	// Round-trip contract (see frontend wireInputFilters): every control lives
	// inside a wrapper carrying data-vibe-input=<variable name>. The wrapper also
	// declares which value the toggle maps to in its two states so an unchecked
	// toggle has a well-defined param mapping (default: checked => "true",
	// unchecked => "" which the collector treats as "remove the param" =>
	// unfiltered). text_input maps an empty string the same way (absent param =>
	// unfiltered). This makes the round-trip well-defined for every control type.
	checkedValue := getStringConfig(node.Config, "checked_value")
	if checkedValue == "" {
		checkedValue = "true"
	}
	uncheckedValue := getStringConfig(node.Config, "unchecked_value") // default "" => param removed

	var b strings.Builder
	b.WriteString("<div class=\"vibe-input\" data-vibe-input=\"" + html.EscapeString(name) + "\"")
	if node.Type == "toggle" {
		b.WriteString(attr("data-vibe-checked-value", checkedValue))
		b.WriteString(" data-vibe-unchecked-value=\"" + html.EscapeString(uncheckedValue) + "\"")
	}
	b.WriteString(">")
	if title != "" {
		b.WriteString("<label class=\"vibe-input-label\">" + html.EscapeString(title) + "</label>")
	}

	switch node.Type {
	case "button_group":
		b.WriteString("<div class=\"vibe-button-group\" role=\"group\">")
		for _, opt := range inputOptions(node, rrs) {
			active := ""
			if opt == current {
				active = " active"
			}
			b.WriteString(fmt.Sprintf(
				"<button type=\"button\" class=\"vibe-button-group-item%s\" data-vibe-value=\"%s\">%s</button>",
				active, html.EscapeString(opt), html.EscapeString(opt)))
		}
		b.WriteString("</div>")

	case "slider":
		min := getStringConfig(node.Config, "min")
		max := getStringConfig(node.Config, "max")
		step := getStringConfig(node.Config, "step")
		val := current
		if val == "" {
			val = getStringConfig(node.Config, "default")
		}
		b.WriteString(fmt.Sprintf(
			"<input type=\"range\" class=\"vibe-slider\"%s%s%s%s />",
			attr("min", min), attr("max", max), attr("step", step), attr("value", val)))

	case "text_input":
		placeholder := getStringConfig(node.Config, "placeholder")
		b.WriteString(fmt.Sprintf(
			"<input type=\"text\" class=\"vibe-text-input\"%s%s />",
			attr("placeholder", placeholder), attr("value", current)))

	case "toggle":
		checked := ""
		if current == checkedValue || current == "true" || current == "1" {
			checked = " checked"
		}
		b.WriteString("<input type=\"checkbox\" class=\"vibe-toggle\"" + checked + " />")

	default: // "dropdown"
		b.WriteString("<select class=\"vibe-dropdown\">")
		for _, opt := range inputOptions(node, rrs) {
			selected := ""
			if opt == current {
				selected = " selected"
			}
			b.WriteString(fmt.Sprintf(
				"<option value=\"%s\"%s>%s</option>",
				html.EscapeString(opt), selected, html.EscapeString(opt)))
		}
		b.WriteString("</select>")
	}

	b.WriteString("</div>")
	return b.String()
}

// inputOptions extracts the distinct option values for an input control from its
// bound source result set, reading the column named by the directive's `column`
// config (defaulting to the first column).
func inputOptions(node *pages.DirectiveNode, rrs *renderer.ResultSet) []string {
	if rrs == nil || len(rrs.Rows) == 0 {
		return nil
	}
	col := getStringConfig(node.Config, "column")
	idx := 0
	if col != "" {
		for i, c := range rrs.Columns {
			if c.Name == col {
				idx = i
				break
			}
		}
	}
	opts := make([]string, 0, len(rrs.Rows))
	for _, row := range rrs.Rows {
		if idx >= len(row) || row[idx] == nil {
			continue
		}
		opts = append(opts, fmt.Sprintf("%v", row[idx]))
	}
	return opts
}

// attr renders an optional HTML attribute, emitting nothing when the value is
// empty so controls degrade cleanly when config is omitted.
func attr(name, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf(" %s=\"%s\"", name, html.EscapeString(value))
}

// substituteInlineValues replaces resolved inline {value:} placeholders in
// already-rendered HTML with formatted inline <span> elements. Each value is
// formatted via the shared renderer.Format codes; a nil value (missing column /
// empty result) renders as a safe em-dash placeholder rather than crashing.
func substituteInlineValues(rendered string, values []pages.InlineValue) string {
	for _, iv := range values {
		var text string
		if iv.Value == nil {
			text = "—" // em-dash placeholder for missing/empty values
		} else if iv.Fmt != "" {
			text = renderer.Format(iv.Value, iv.Fmt)
		} else {
			text = fmt.Sprintf("%v", iv.Value)
		}
		span := "<span class=\"vibe-value\">" + html.EscapeString(text) + "</span>"
		rendered = strings.ReplaceAll(rendered, iv.Placeholder, span)
	}
	return rendered
}

// metricPriorValue resolves the comparison baseline from a result set. It prefers
// a column named "prior" (case-insensitive) in the first row, else falls back to
// the second column of the first row.
func metricPriorValue(rrs *renderer.ResultSet) any {
	if rrs == nil || len(rrs.Rows) == 0 {
		return nil
	}
	row := rrs.Rows[0]
	for i, col := range rrs.Columns {
		if i < len(row) && strings.EqualFold(col.Name, "prior") {
			return row[i]
		}
	}
	if len(row) > 1 {
		return row[1]
	}
	return nil
}

// metricSeries extracts a numeric series for a sparkline. For a multi-column
// result it uses the last column (the measure in a time-series); for a
// single-column result it uses that column.
func metricSeries(rrs *renderer.ResultSet) []float64 {
	if rrs == nil || len(rrs.Columns) == 0 {
		return nil
	}
	col := len(rrs.Columns) - 1
	series := make([]float64, 0, len(rrs.Rows))
	for _, row := range rrs.Rows {
		if col < len(row) {
			series = append(series, renderer.ToFloat64(row[col]))
		}
	}
	return series
}

// getIntConfig extracts an int from a config map value, tolerating the numeric
// types YAML decoding may produce (int, int64, float64).
func getIntConfig(config map[string]any, key string) int {
	v, ok := config[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// getBoolConfig extracts a bool from a config map value, tolerating string forms.
func getBoolConfig(config map[string]any, key string) bool {
	v, ok := config[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	default:
		return false
	}
}

// getStackMode reads a stacking config value that may be a bool or a string.
// It returns "" (no stacking), "true" (plain stacking), or "100%" (normalized
// stacking). Any truthy string other than "100%" maps to "true".
func getStackMode(config map[string]any, key string) string {
	v, ok := config[key]
	if !ok || v == nil {
		return ""
	}
	switch b := v.(type) {
	case bool:
		if b {
			return "true"
		}
		return ""
	case string:
		s := strings.TrimSpace(b)
		if s == "100%" {
			return "100%"
		}
		if strings.EqualFold(s, "true") {
			return "true"
		}
		return ""
	default:
		return ""
	}
}

// getBoolPtrConfig reads an optional bool. It returns nil when the key is
// absent so callers can distinguish "unset" from "false".
func getBoolPtrConfig(config map[string]any, key string) *bool {
	v, ok := config[key]
	if !ok || v == nil {
		return nil
	}
	var b bool
	switch t := v.(type) {
	case bool:
		b = t
	case string:
		b = strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return nil
	}
	return &b
}

// toFloatPtr converts a YAML-decoded scalar to a *float64, or nil if it is not
// numeric.
func toFloatPtr(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(n), "%g", &f); err == nil {
			return &f
		}
	}
	return nil
}

// getFloatRangeConfig reads a [min, max] numeric range (e.g. y_range: [0, 100]).
// Either bound may be null to leave it auto.
func getFloatRangeConfig(config map[string]any, key string) [2]*float64 {
	var out [2]*float64
	v, ok := config[key]
	if !ok || v == nil {
		return out
	}
	list, ok := v.([]any)
	if !ok {
		return out
	}
	if len(list) > 0 {
		out[0] = toFloatPtr(list[0])
	}
	if len(list) > 1 {
		out[1] = toFloatPtr(list[1])
	}
	return out
}

// getStringSliceConfig reads a YAML flow list of strings (e.g. colors: [a, b]).
func getStringSliceConfig(config map[string]any, key string) []string {
	v, ok := config[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprintf("%v", item))
			}
		}
		return out
	default:
		return nil
	}
}

// getRefLinesConfig reads ref_lines: [{value: 50000, label: "Target"}] into a
// slice of renderer.RefLine.
func getRefLinesConfig(config map[string]any, key string) []renderer.RefLine {
	v, ok := config[key]
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]renderer.RefLine, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rl := renderer.RefLine{}
		if fp := toFloatPtr(m["value"]); fp != nil {
			rl.Value = *fp
		}
		if s, ok := m["label"].(string); ok {
			rl.Label = s
		}
		out = append(out, rl)
	}
	return out
}

func getStringConfig(config map[string]any, key string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringOrSliceConfig reads a config value that may be either a scalar string
// or a YAML flow list. A scalar (e.g. "revenue") is returned as the single
// value with a nil slice; a list (e.g. [revenue, cost]) is returned as the slice
// with an empty single value. This lets the chart directive accept both
// `y: revenue` (single-series) and `y: [revenue, cost]` (multi-series).
func getStringOrSliceConfig(config map[string]any, key string) (single string, list []string) {
	v, ok := config[key]
	if !ok || v == nil {
		return "", nil
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				list = append(list, s)
			} else {
				list = append(list, fmt.Sprintf("%v", item))
			}
		}
		return "", list
	case []string:
		return "", t
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// getStringMapConfig extracts a map[string]string from a config map value.
// Handles both map[string]any and map[string]string input types.
func getStringMapConfig(config map[string]any, key string) map[string]string {
	v, ok := config[key]
	if !ok || v == nil {
		return nil
	}
	switch m := v.(type) {
	case map[string]string:
		return m
	case map[string]any:
		result := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		return result
	default:
		return nil
	}
}

// CreatePage creates a new page and returns its ID.
func (a *App) CreatePage(title, content string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}

	id := uuid.New().String()
	db := a.container.SQLiteDB()
	_, err := db.Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, ?, ?)",
		id, title, content, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}
	return id, nil
}

// CreateHTMLPage creates a new HTML page and returns its ID.
func (a *App) CreateHTMLPage(title, content string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}

	id := uuid.New().String()
	db := a.container.SQLiteDB()
	_, err := db.Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'html', 0, ?, ?)",
		id, title, content, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create html page: %w", err)
	}
	return id, nil
}

// UpdatePage updates a page's title and content.
func (a *App) UpdatePage(id, title, content string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	_, err := db.Exec(
		"UPDATE _vibe_pages SET title = ?, content = ?, updated_at = ? WHERE id = ?",
		title, content, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update page: %w", err)
	}
	return nil
}

// DeletePage deletes a page by ID.
func (a *App) DeletePage(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	_, err := db.Exec("DELETE FROM _vibe_pages WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete page: %w", err)
	}
	return nil
}

// --- Tables ---

// ListTables returns all user tables (non _vibe_ tables).
func (a *App) ListTables() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	rows, err := db.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// ColumnInfo describes a table column.
type ColumnInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"not_null"`
	PK      bool   `json:"pk"`
}

// TableInfo describes a table's schema.
type TableInfo struct {
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
}

// DescribeTable returns schema info for a table.
func (a *App) DescribeTable(name string) (*TableInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	rows, err := db.Query(
		"SELECT column_name, data_type, CASE WHEN is_nullable = 'NO' THEN true ELSE false END AS not_null FROM information_schema.columns WHERE table_schema = 'main' AND table_name = $1 ORDER BY ordinal_position",
		name)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer rows.Close()

	info := &TableInfo{Name: name}
	for rows.Next() {
		var colName, colType string
		var notNull bool
		if err := rows.Scan(&colName, &colType, &notNull); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		info.Columns = append(info.Columns, ColumnInfo{
			Name:    colName,
			Type:    colType,
			NotNull: notNull,
			PK:      false,
		})
	}
	return info, rows.Err()
}

// TableData holds paginated table data.
type TableData struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalRows  int      `json:"total_rows"`
	TotalPages int      `json:"total_pages"`
}

// GetTableData returns paginated data from a table.
func (a *App) GetTableData(name string, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	var totalRows int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", name)).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && isValidIdentifier(sortCol) {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %q %s", sortCol, dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %q%s LIMIT ? OFFSET ?", name, orderClause)
	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		// Convert []byte to string for JSON serialization.
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		data = append(data, values)
	}

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	return &TableData{
		Columns:    columns,
		Rows:       data,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}, rows.Err()
}

// FilterCondition is one structured datasheet filter clause (plan §2.3). It is
// the SQL-free unit behind Filter-by-Selection (a single eq) and Filter-by-Form
// (multiple conditions). Op is one of eq/neq/lt/gt/lte/gte/like/is_null/
// is_not_null; Value is ignored for the is_null/is_not_null operators.
type FilterCondition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// FilterSpec is a set of FilterConditions joined by a single combinator
// ("AND" or "OR"). It is translated to a parameterized WHERE clause.
type FilterSpec struct {
	Combinator string            `json:"combinator"`
	Conditions []FilterCondition `json:"conditions"`
}

// GetTableDataFiltered returns paginated, structured-filter-driven table data
// plus an accurate total-row count for the record-count bar (plan §2.3, P2.5).
//
// Translation rules:
//   - each condition's column is validated against the table's real columns
//     (case-sensitive ident allowlist) — no SQL injection via identifier
//   - values are bound as parameters (?), never interpolated — no injection via
//     value (a single quote matches literally)
//   - is_null / is_not_null are value-less and bind no parameter
//   - conditions join with the spec combinator (AND/OR; defaults to AND)
func (a *App) GetTableDataFiltered(name string, spec FilterSpec, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Build an allowlist of real columns to validate filter/sort identifiers.
	cols, err := tableColumns(db, name)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	whereClause, args, err := buildFilterWhere(spec, known)
	if err != nil {
		return nil, err
	}

	var totalRows int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %q%s", name, whereClause)
	if err := db.QueryRow(countQuery, args...).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count filtered rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && isValidIdentifier(sortCol) && known[sortCol] {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %q %s", sortCol, dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %q%s%s LIMIT ? OFFSET ?", name, whereClause, orderClause)
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		data = append(data, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate filtered rows: %w", err)
	}

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	return &TableData{
		Columns:    columns,
		Rows:       data,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}, nil
}

// filterOps maps a FilterCondition.Op to its SQL comparison operator. Value-less
// operators (is_null/is_not_null) are handled separately.
var filterOps = map[string]string{
	"eq":   "=",
	"neq":  "<>",
	"lt":   "<",
	"gt":   ">",
	"lte":  "<=",
	"gte":  ">=",
	"like": "LIKE",
}

// buildFilterWhere translates a FilterSpec into a parameterized WHERE clause
// (leading " WHERE ...", or "" when there are no conditions) plus its bound
// args. Column identifiers are validated against the known-columns allowlist;
// values are always bound, never interpolated.
func buildFilterWhere(spec FilterSpec, known map[string]bool) (string, []any, error) {
	if len(spec.Conditions) == 0 {
		return "", nil, nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	var args []any
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", nil, fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%q IS NULL", c.Column))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%q IS NOT NULL", c.Column))
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", nil, fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%q %s ?", c.Column, sqlOp))
			args = append(args, c.Value)
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), args, nil
}

// FormSubmitRow describes one row to write as part of an atomic form submit.
type FormSubmitRow struct {
	Table  string         `json:"table"`
	Values map[string]any `json:"values"`
}

// InsertRow inserts a row into a table, coercing all-string form inputs to the
// column's type (via information_schema) and enforcing the Access-style
// _vibe_field_meta required + validation_rule constraints server-side. On a
// validation failure it returns a field-keyed *WriteError so the form UI can
// highlight the offending controls instead of surfacing a raw SQL string.
func (a *App) InsertRow(table string, values map[string]any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ddb, colTypes, metas, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := insertRowTx(tx, colTypes, metas, table, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit insert: %w", err)
	}
	return res, nil
}

// UpdateRow updates a row in a table by primary key, applying the same coercion
// and validation as InsertRow. A 0-affected-row outcome (PK mismatch) is
// surfaced as an error rather than a silent no-op.
func (a *App) UpdateRow(table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !isValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, metas, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := updateRowTx(tx, colTypes, metas, table, pkCol, pkVal, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit update: %w", err)
	}
	return res, nil
}

// SubmitForm writes a parent row plus zero or more child rows as a single
// atomic logical submit (Access-style form + subform). All writes run in one
// DuckDB transaction: if any child fails coercion/validation, the whole submit
// rolls back so no orphaned parent is left behind.
func (a *App) SubmitForm(parent FormSubmitRow, children []FormSubmitRow) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	tx, err := ddb.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	rows := append([]FormSubmitRow{parent}, children...)
	for _, row := range rows {
		if !isValidIdentifier(row.Table) {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("invalid table name")
		}
		colTypes, err := columnTypes(ddb, row.Table)
		if err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		metas, err := fieldMetaMap(schema.NewStore(a.container.SQLiteDB()), row.Table)
		if err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		if _, err := insertRowTx(tx, colTypes, metas, row.Table, row.Values); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit form submit: %w", err)
	}
	return nil
}

// prepareWrite validates the open container + table and loads the column types
// and field metadata needed by the transactional write helpers.
func (a *App) prepareWrite(table string) (*sql.DB, map[string]string, map[string]schema.FieldMeta, error) {
	if a.container == nil {
		return nil, nil, nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return nil, nil, nil, fmt.Errorf("invalid table name")
	}
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return nil, nil, nil, fmt.Errorf("DuckDB not available")
	}
	colTypes, err := columnTypes(ddb, table)
	if err != nil {
		return nil, nil, nil, err
	}
	metas, err := fieldMetaMap(schema.NewStore(a.container.SQLiteDB()), table)
	if err != nil {
		return nil, nil, nil, err
	}
	return ddb, colTypes, metas, nil
}

// DeleteRow deletes a row from a table by primary key. Consistent with
// InsertRow/UpdateRow, it validates the pk column identifier, runs the DELETE
// inside a transaction with rollback on error, coerces pkVal to the PK column's
// type, and surfaces a 0-affected-row outcome (PK mismatch) as a structured
// *WriteError rather than a raw SQL string.
func (a *App) DeleteRow(table, pkCol string, pkVal any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !isValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, _, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := deleteRowTx(tx, colTypes, table, pkCol, pkVal)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit delete: %w", err)
	}
	return res, nil
}

// CreateTable creates a new table with the given columns.
func (a *App) CreateTable(name string, columns []ColumnInfo) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid table name")
	}

	var colDefs []string
	for _, col := range columns {
		def := fmt.Sprintf("%q %s", col.Name, col.Type)
		if col.PK {
			def += " PRIMARY KEY"
		}
		if col.NotNull {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}

	query := fmt.Sprintf("CREATE TABLE %q (%s)", name, strings.Join(colDefs, ", "))
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// ForeignKeySpec describes a foreign-key relationship to add (P2.3): the child
// column references the parent column, with optional cascade options.
type ForeignKeySpec struct {
	ChildTable   string `json:"child_table"`
	ChildColumn  string `json:"child_column"`
	ParentTable  string `json:"parent_table"`
	ParentColumn string `json:"parent_column"`
	OnDelete     string `json:"on_delete"`
	OnUpdate     string `json:"on_update"`
}

// AddForeignKey declares a foreign-key relationship (P2.3): the child column
// references the parent column. Because DuckDB does not support ALTER TABLE ...
// ADD FOREIGN KEY (see internal/engine relationship spike §8/§4.1), the child
// table is recreated with the FK declared inline (copy rows into a rebuilt table
// that carries the inline REFERENCES, then swap). DuckDB FKs are RESTRICT-only,
// which matches the Access "enforce referential integrity" default. The
// relationship metadata is persisted into _vibe_relationships as the source of
// truth. All validation runs BEFORE any mutation, so a rejected call leaves no
// constraint and no metadata row behind.
func (a *App) AddForeignKey(spec ForeignKeySpec) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}

	for _, id := range []string{spec.ChildTable, spec.ChildColumn, spec.ParentTable, spec.ParentColumn} {
		if !isValidIdentifier(id) {
			return "", fmt.Errorf("invalid identifier %q in foreign key spec", id)
		}
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	// Validate the child column exists.
	childCols, err := tableColumns(ddb, spec.ChildTable)
	if err != nil {
		return "", fmt.Errorf("failed to inspect child table %q: %w", spec.ChildTable, err)
	}
	if len(childCols) == 0 {
		return "", fmt.Errorf("child table %q does not exist", spec.ChildTable)
	}
	if !columnExists(childCols, spec.ChildColumn) {
		return "", fmt.Errorf("child column %q does not exist on table %q", spec.ChildColumn, spec.ChildTable)
	}

	// Validate the parent column exists.
	parentCols, err := tableColumns(ddb, spec.ParentTable)
	if err != nil {
		return "", fmt.Errorf("failed to inspect parent table %q: %w", spec.ParentTable, err)
	}
	if len(parentCols) == 0 {
		return "", fmt.Errorf("parent table %q does not exist", spec.ParentTable)
	}
	if !columnExists(parentCols, spec.ParentColumn) {
		return "", fmt.Errorf("parent column %q does not exist on table %q", spec.ParentColumn, spec.ParentTable)
	}

	// DuckDB requires the referenced (parent) column to be UNIQUE or PRIMARY KEY.
	unique, err := columnIsUnique(ddb, spec.ParentTable, spec.ParentColumn)
	if err != nil {
		return "", fmt.Errorf("failed to inspect parent constraints on %q: %w", spec.ParentTable, err)
	}
	if !unique {
		return "", fmt.Errorf("referenced column %q.%q must be a PRIMARY KEY or UNIQUE column", spec.ParentTable, spec.ParentColumn)
	}

	// Recreate the child table with the inline REFERENCES, since DuckDB cannot
	// ALTER a FK onto an existing table.
	if err := rebuildTableWithFK(ddb, spec, childCols); err != nil {
		return "", fmt.Errorf("failed to apply foreign key to %q: %w", spec.ChildTable, err)
	}

	rel := schema.Relationship{
		ID:               uuid.New().String(),
		ParentTable:      spec.ParentTable,
		ParentColumn:     spec.ParentColumn,
		ChildTable:       spec.ChildTable,
		ChildColumn:      spec.ChildColumn,
		OnDelete:         normalizeAction(spec.OnDelete),
		OnUpdate:         normalizeAction(spec.OnUpdate),
		EnforceIntegrity: true,
	}
	store := schema.NewRelationshipStore(a.container.SQLiteDB())
	if err := store.Save(rel); err != nil {
		return "", fmt.Errorf("failed to persist relationship metadata: %w", err)
	}

	a.logger.Info("added foreign key",
		"child_table", spec.ChildTable, "child_column", spec.ChildColumn,
		"parent_table", spec.ParentTable, "parent_column", spec.ParentColumn,
		"id", rel.ID)
	return rel.ID, nil
}

// ListRelationships returns every stored foreign-key relationship definition.
func (a *App) ListRelationships() ([]schema.Relationship, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	store := schema.NewRelationshipStore(a.container.SQLiteDB())
	rels, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list relationships: %w", err)
	}
	return rels, nil
}

// RemoveForeignKey drops the foreign-key constraint identified by id: the child
// table is rebuilt WITHOUT the inline REFERENCES (since DuckDB cannot ALTER a FK
// off an existing table) and the relationship metadata row is deleted.
func (a *App) RemoveForeignKey(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	store := schema.NewRelationshipStore(a.container.SQLiteDB())
	rel, err := store.Get(id)
	if err != nil {
		return fmt.Errorf("failed to load relationship %q: %w", id, err)
	}
	if rel == nil {
		return fmt.Errorf("relationship %q not found", id)
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	childCols, err := tableColumns(ddb, rel.ChildTable)
	if err != nil {
		return fmt.Errorf("failed to inspect child table %q: %w", rel.ChildTable, err)
	}
	if len(childCols) > 0 {
		// Rebuild the child table with no FK spec to strip the inline REFERENCES.
		if err := rebuildTableWithFK(ddb, ForeignKeySpec{ChildTable: rel.ChildTable}, childCols); err != nil {
			return fmt.Errorf("failed to drop foreign key from %q: %w", rel.ChildTable, err)
		}
	}

	if err := store.Delete(id); err != nil {
		return fmt.Errorf("failed to delete relationship metadata %q: %w", id, err)
	}

	a.logger.Info("removed foreign key", "id", id, "child_table", rel.ChildTable)
	return nil
}

// fkColumn describes a column for table reconstruction (P2.3).
type fkColumn struct {
	Name    string
	Type    string
	NotNull bool
	PK      bool
}

// tableColumns introspects a DuckDB table's columns (name, type, nullability,
// primary-key membership) in ordinal order. Returns an empty slice if the table
// does not exist.
func tableColumns(db *sql.DB, table string) ([]fkColumn, error) {
	rows, err := db.Query(
		`SELECT column_name, data_type, CASE WHEN is_nullable = 'NO' THEN true ELSE false END
		   FROM information_schema.columns
		  WHERE table_schema = 'main' AND table_name = $1
		  ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	var cols []fkColumn
	for rows.Next() {
		var c fkColumn
		if err := rows.Scan(&c.Name, &c.Type, &c.NotNull); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate columns: %w", err)
	}

	pk, err := primaryKeyColumns(db, table)
	if err != nil {
		return nil, err
	}
	for i := range cols {
		if pk[cols[i].Name] {
			cols[i].PK = true
		}
	}
	return cols, nil
}

// primaryKeyColumns returns the set of PRIMARY KEY column names for a table.
func primaryKeyColumns(db *sql.DB, table string) (map[string]bool, error) {
	return constraintColumns(db, table, "PRIMARY KEY")
}

// columnIsUnique reports whether the given column is covered by a single-column
// PRIMARY KEY or UNIQUE constraint (DuckDB's requirement for a FK target).
func columnIsUnique(db *sql.DB, table, column string) (bool, error) {
	for _, ct := range []string{"PRIMARY KEY", "UNIQUE"} {
		cols, err := constraintColumns(db, table, ct)
		if err != nil {
			return false, err
		}
		if cols[column] {
			return true, nil
		}
	}
	return false, nil
}

// constraintColumns returns the set of column names participating in any
// constraint of the given type on the table, using DuckDB's duckdb_constraints()
// table function.
func constraintColumns(db *sql.DB, table, constraintType string) (map[string]bool, error) {
	rows, err := db.Query(
		`SELECT constraint_column_names FROM duckdb_constraints()
		  WHERE table_name = $1 AND constraint_type = $2`, table, constraintType)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s constraints: %w", constraintType, err)
	}
	defer rows.Close()

	set := map[string]bool{}
	for rows.Next() {
		var names []any
		if err := rows.Scan(&names); err != nil {
			return nil, fmt.Errorf("failed to scan constraint columns: %w", err)
		}
		for _, n := range names {
			if s, ok := n.(string); ok {
				set[s] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate constraints: %w", err)
	}
	return set, nil
}

// rebuildTableWithFK recreates a child table with the same columns, preserving
// types / NOT NULL / PRIMARY KEY, optionally adding an inline FK REFERENCES on
// spec.ChildColumn (when spec.ParentTable is set). Existing rows are copied into
// the rebuilt table, then the original is dropped and the rebuild renamed into
// place. Done in a single transaction so a failure (e.g. an FK that orphan rows
// would violate) leaves the original table untouched.
func rebuildTableWithFK(db *sql.DB, spec ForeignKeySpec, cols []fkColumn) error {
	tmp := spec.ChildTable + "__fk_rebuild"

	var defs []string
	for _, c := range cols {
		def := fmt.Sprintf("%q %s", c.Name, c.Type)
		if c.PK {
			def += " PRIMARY KEY"
		}
		if c.NotNull && !c.PK {
			def += " NOT NULL"
		}
		if spec.ParentTable != "" && c.Name == spec.ChildColumn {
			def += fmt.Sprintf(" REFERENCES %q(%q)", spec.ParentTable, spec.ParentColumn)
		}
		defs = append(defs, def)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin rebuild transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q", tmp)); err != nil {
		return fmt.Errorf("failed to drop stale rebuild table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("CREATE TABLE %q (%s)", tmp, strings.Join(defs, ", "))); err != nil {
		return fmt.Errorf("failed to create rebuild table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO %q SELECT * FROM %q", tmp, spec.ChildTable)); err != nil {
		return fmt.Errorf("failed to copy rows into rebuild table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %q", spec.ChildTable)); err != nil {
		return fmt.Errorf("failed to drop original table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %q RENAME TO %q", tmp, spec.ChildTable)); err != nil {
		return fmt.Errorf("failed to rename rebuild table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit table rebuild: %w", err)
	}
	return nil
}

// columnExists reports whether a column of the given name is present.
func columnExists(cols []fkColumn, name string) bool {
	for _, c := range cols {
		if c.Name == name {
			return true
		}
	}
	return false
}

// normalizeAction normalizes a referential action. DuckDB FKs are effectively
// RESTRICT-only; any unrecognized/empty value is stored as the Access default
// "RESTRICT" so the persisted metadata reflects the enforced behavior.
func normalizeAction(action string) string {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "CASCADE":
		return "CASCADE"
	case "SET NULL":
		return "SET NULL"
	case "NO ACTION":
		return "NO ACTION"
	default:
		return "RESTRICT"
	}
}

// DropTable drops a table.
func (a *App) DropTable(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid table name")
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q", name))
	if err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}
	return nil
}

// --- Queries ---

// QueryResult holds the results of an executed query.
type QueryResult struct {
	Columns  []ColumnInfo `json:"columns"`
	Rows     [][]any      `json:"rows"`
	Duration string       `json:"duration"`
	RowCount int          `json:"row_count"`
}

// ExecuteQuery runs a SQL query on DuckDB.
func (a *App) ExecuteQuery(sqlStr string) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	start := time.Now()
	rs, err := a.engine.Execute(context.Background(), sqlStr, nil)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	result := &QueryResult{
		Duration: duration.String(),
		RowCount: rs.RowCount(),
	}

	for _, col := range rs.Columns {
		result.Columns = append(result.Columns, ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		})
	}

	// Convert []byte to string for JSON.
	for _, row := range rs.Rows {
		cleaned := make([]any, len(row))
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				cleaned[i] = string(b)
			} else {
				cleaned[i] = v
			}
		}
		result.Rows = append(result.Rows, cleaned)
	}

	return result, nil
}

// QuerySummary holds a query list entry.
type QuerySummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Engine string `json:"engine"`
}

// ListQueries returns all saved queries.
func (a *App) ListQueries() ([]QuerySummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	qs, err := a.registry.List(context.Background())
	if err != nil {
		return nil, err
	}

	result := make([]QuerySummary, 0, len(qs))
	for _, q := range qs {
		result = append(result, QuerySummary{
			ID:     q.ID,
			Title:  q.Title,
			Engine: q.Engine,
		})
	}
	return result, nil
}

// GetQuery returns a saved query by ID.
func (a *App) GetQuery(id string) (*queries.Query, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	return a.registry.Get(context.Background(), id)
}

// SaveQuery creates or updates a saved query.
func (a *App) SaveQuery(id, title, sqlStr, description string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	if id == "" {
		id = uuid.New().String()
	}

	q := &queries.Query{
		ID:         id,
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}

	// Try update first, create if not found.
	err := a.registry.Update(context.Background(), q)
	if err != nil {
		return a.registry.Create(context.Background(), q)
	}
	return nil
}

// PreviewFilterSQL renders a FilterSpec (plus optional selected columns from the
// QBE grid) into a deterministic SQL string WITHOUT persisting it (plan §2.4,
// P2.6). The generated SQL is the single rendering path shared with
// SaveFilterAsQuery, so the preview shown to the builder is byte-for-byte the
// SQL that gets stored.
func (a *App) PreviewFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.generateFilterSQL(name, spec, columns)
}

// SaveFilterAsQuery renders a FilterSpec into deterministic SQL and persists it
// as a named query via the existing queries.Registry, returning the new query id
// (plan §2.4, P2.6). It reuses the same rendering path as PreviewFilterSQL so the
// stored SQL equals the previewed SQL.
func (a *App) SaveFilterAsQuery(name string, spec FilterSpec, columns []string, title string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sqlStr, err := a.generateFilterSQL(name, spec, columns)
	if err != nil {
		return "", err
	}

	id := uuid.New().String()
	q := &queries.Query{
		ID:         id,
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := a.registry.Create(context.Background(), q); err != nil {
		return "", fmt.Errorf("failed to save filter as query: %w", err)
	}
	a.logger.Info("saved filter as named query", "id", id, "table", name, "title", title)
	return id, nil
}

// generateFilterSQL is the single, deterministic FilterSpec → SQL renderer behind
// both PreviewFilterSQL and SaveFilterAsQuery. Unlike GetTableDataFiltered (which
// binds parameters at execution time), a saved query is a standalone SQL string,
// so literal values are safely inlined here. Column identifiers (filter columns
// and the optional projection) are validated against the table's real columns —
// no SQL injection via identifier or value. The caller must hold a.mu.
func (a *App) generateFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return "", fmt.Errorf("invalid table name")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	cols, err := tableColumns(db, name)
	if err != nil {
		return "", fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	// Projection: SELECT * unless the QBE grid selected explicit columns.
	projection := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		for _, col := range columns {
			if !known[col] {
				return "", fmt.Errorf("unknown selected column %q", col)
			}
			parts = append(parts, fmt.Sprintf("%q", col))
		}
		projection = strings.Join(parts, ", ")
	}

	whereClause, err := buildFilterWhereInline(spec, known)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT %s FROM %q%s", projection, name, whereClause), nil
}

// buildFilterWhereInline mirrors buildFilterWhere but inlines literal values into
// the SQL (for a persistable, standalone query string) instead of binding them as
// parameters. Identifiers are validated against the allowlist; string literals are
// single-quote escaped so a value can never break out of the literal.
func buildFilterWhereInline(spec FilterSpec, known map[string]bool) (string, error) {
	if len(spec.Conditions) == 0 {
		return "", nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%q IS NULL", c.Column))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%q IS NOT NULL", c.Column))
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%q %s %s", c.Column, sqlOp, sqlLiteral(c.Value)))
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), nil
}

// sqlLiteral renders a FilterCondition value as a safe inline SQL literal. Numeric
// and boolean values render bare; everything else (including string-typed numeric
// inputs from the all-string form path) renders as a single-quote-escaped string
// literal, which DuckDB coerces to the column type on comparison.
func sqlLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", t)
	case float32, float64:
		return fmt.Sprintf("%v", t)
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", t), "'", "''") + "'"
	}
}

// DeleteQuery deletes a saved query.
func (a *App) DeleteQuery(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	return a.registry.Delete(context.Background(), id)
}

// --- Dashboard ---

// DashboardData holds overview stats.
type DashboardData struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	PageCount   int    `json:"page_count"`
	TableCount  int    `json:"table_count"`
	QueryCount  int    `json:"query_count"`
	FileSize    int64  `json:"file_size"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// GetDashboard returns dashboard overview data.
func (a *App) GetDashboard() (*DashboardData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	d := &DashboardData{}

	if v, err := a.container.Meta("title"); err == nil {
		d.Title = v
	}
	if v, err := a.container.Meta("description"); err == nil {
		d.Description = v
	}
	if v, err := a.container.Meta("created_at"); err == nil {
		d.CreatedAt = v
	}
	if v, err := a.container.Meta("updated_at"); err == nil {
		d.UpdatedAt = v
	}

	db := a.container.SQLiteDB()
	db.QueryRow("SELECT COUNT(*) FROM _vibe_pages").Scan(&d.PageCount)    //nolint:errcheck
	db.QueryRow("SELECT COUNT(*) FROM _vibe_queries").Scan(&d.QueryCount) //nolint:errcheck

	if duckDB := a.container.DuckDBDB(); duckDB != nil {
		duckDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main'").Scan(&d.TableCount) //nolint:errcheck
	}

	if fi, err := os.Stat(a.filePath); err == nil {
		d.FileSize = fi.Size()
	}

	return d, nil
}

// --- Agent ---

// AgentResult holds the result of an agent query.
type AgentResult struct {
	SQL      string   `json:"sql,omitempty"`
	Columns  []string `json:"columns,omitempty"`
	Rows     [][]any  `json:"rows,omitempty"`
	Analysis string   `json:"analysis,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// SendAgentQuery executes a SQL query directly (MVP — no NL support yet).
func (a *App) SendAgentQuery(query string) (*AgentResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	rs, err := a.engine.Execute(context.Background(), query, nil)
	if err != nil {
		return &AgentResult{Error: err.Error()}, nil
	}

	result := &AgentResult{SQL: query}
	for _, col := range rs.Columns {
		result.Columns = append(result.Columns, col.Name)
	}
	for _, row := range rs.Rows {
		cleaned := make([]any, len(row))
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				cleaned[i] = string(b)
			} else {
				cleaned[i] = v
			}
		}
		result.Rows = append(result.Rows, cleaned)
	}
	return result, nil
}

// GetAgentStatus returns agent status info.
func (a *App) GetAgentStatus() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	status := map[string]any{
		"available": a.container != nil,
		"mode":      "read-only",
	}
	return status
}

// --- Import/Export ---

// SelectCSVFile opens a file dialog to select a CSV file.
func (a *App) SelectCSVFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import CSV",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// ImportResult holds CSV import results.
type ImportResult struct {
	RowsImported int    `json:"rows_imported"`
	TableName    string `json:"table_name"`
}

// ImportCSV imports a CSV file into a table.
func (a *App) ImportCSV(csvPath, tableName string) (*ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return nil, fmt.Errorf("invalid table name")
	}

	data, err := os.ReadFile(csvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("CSV file is empty or has no data rows")
	}

	// Parse header.
	headers := strings.Split(lines[0], ",")
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Check if table exists; if not, create it.
	var exists int
	db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main' AND table_name = $1", tableName).Scan(&exists) //nolint:errcheck

	if exists == 0 {
		var colDefs []string
		for _, h := range headers {
			colDefs = append(colDefs, fmt.Sprintf("%q TEXT", h))
		}
		createSQL := fmt.Sprintf("CREATE TABLE %q (%s)", tableName, strings.Join(colDefs, ", "))
		if _, err := db.Exec(createSQL); err != nil {
			return nil, fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Insert rows.
	placeholders := make([]string, len(headers))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %q VALUES (%s)", tableName, strings.Join(placeholders, ", "))

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var imported int
	for _, line := range lines[1:] {
		fields := strings.Split(line, ",")
		args := make([]any, len(headers))
		for i := range headers {
			if i < len(fields) {
				args[i] = strings.TrimSpace(fields[i])
			} else {
				args[i] = ""
			}
		}
		if _, err := stmt.Exec(args...); err != nil {
			tx.Rollback() //nolint:errcheck
			return nil, fmt.Errorf("failed to insert row: %w", err)
		}
		imported++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return &ImportResult{RowsImported: imported, TableName: tableName}, nil
}

// ExportTableCSV exports a table as CSV via save dialog.
func (a *App) ExportTableCSV(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return "", fmt.Errorf("invalid table name")
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export CSV",
		DefaultFilename: tableName + ".csv",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	rows, err := ddb.Query(fmt.Sprintf("SELECT * FROM %q", tableName))
	if err != nil {
		return "", fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var b strings.Builder
	b.WriteString(strings.Join(columns, ","))
	b.WriteString("\n")

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}
		fields := make([]string, len(columns))
		for i, v := range values {
			fields[i] = fmt.Sprintf("%v", v)
		}
		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write CSV: %w", err)
	}
	return path, nil
}

// ExportTableJSON exports a table as JSON via save dialog.
func (a *App) ExportTableJSON(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return "", fmt.Errorf("invalid table name")
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export JSON",
		DefaultFilename: tableName + ".json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	rows, err := ddb.Query(fmt.Sprintf("SELECT * FROM %q", tableName))
	if err != nil {
		return "", fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}
		row := make(map[string]any)
		for i, col := range columns {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}
		result = append(result, row)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write JSON: %w", err)
	}
	return path, nil
}

// --- Metadata ---

// GetAllMeta returns all metadata entries.
func (a *App) GetAllMeta() (map[string]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	rows, err := db.Query("SELECT key, value FROM _vibe_meta ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("failed to list meta: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("failed to scan meta: %w", err)
		}
		result[k] = v
	}
	return result, rows.Err()
}

// SetMeta sets a metadata key/value.
func (a *App) SetMeta(key, value string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	return a.container.SetMeta(key, value)
}

// --- Field Metadata (Access-parity, plan §1.3) ---

// GetFieldMeta returns the Access-style field metadata for every column of the
// given table, ordered by sort_order then column_name.
func (a *App) GetFieldMeta(table string) ([]schema.FieldMeta, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	store := schema.NewStore(a.container.SQLiteDB())
	return store.GetFieldMeta(table)
}

// SetFieldMeta upserts the Access-style field metadata for a single
// (table, column) pair.
func (a *App) SetFieldMeta(table, column string, meta schema.FieldMeta) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	store := schema.NewStore(a.container.SQLiteDB())
	return store.SetFieldMeta(table, column, meta)
}

// FormSummary is a lightweight listing entry for _vibe_forms (plan §1.6).
type FormSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Archetype string `json:"archetype"`
}

// SaveForm upserts a form definition into _vibe_forms as versioned JSON.
func (a *App) SaveForm(spec schema.FormSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	store := schema.NewFormStore(a.container.SQLiteDB())
	if err := store.SaveForm(spec); err != nil {
		return fmt.Errorf("failed to save form %q: %w", spec.ID, err)
	}
	return nil
}

// GetForm reads a stored form definition by id from _vibe_forms.
func (a *App) GetForm(id string) (*schema.FormSpec, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	store := schema.NewFormStore(a.container.SQLiteDB())
	return store.GetForm(id)
}

// ListForms returns a summary of every stored form definition.
func (a *App) ListForms() ([]FormSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	store := schema.NewFormStore(a.container.SQLiteDB())
	summaries, err := store.ListForms()
	if err != nil {
		return nil, err
	}
	out := make([]FormSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, FormSummary{ID: s.ID, Title: s.Title, Archetype: s.Archetype})
	}
	return out, nil
}

// --- MCP Server ---

// engineAgentAdapter wraps engine.Manager to satisfy agents.EngineAccess.
type engineAgentAdapter struct {
	mgr *engine.Manager
}

func (ea *engineAgentAdapter) Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error) {
	return ea.mgr.Execute(ctx, sql, params)
}

// StartMCPServer starts the MCP server for the currently open file.
func (a *App) StartMCPServer() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if a.mcpServer != nil {
		return fmt.Sprintf("http://%s/sse", a.mcpServer.Addr()), nil
	}

	sandbox := &agents.SandboxConfig{
		Mode:         agents.ModeFull,
		MaxQueryRows: 10000,
	}
	handler := agents.NewHandler(a.container, &engineAgentAdapter{mgr: a.engine}, sandbox, a.logger)

	srv := vibemcp.New(handler, a.logger)
	if err := srv.Start("127.0.0.1:0"); err != nil {
		return "", fmt.Errorf("failed to start MCP server: %w", err)
	}

	a.mcpServer = srv
	url := fmt.Sprintf("http://%s/sse", srv.Addr())
	a.logger.Info("MCP server started", "url", url)
	return url, nil
}

// StopMCPServer stops the MCP server.
func (a *App) StopMCPServer() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mcpServer == nil {
		return nil
	}
	err := a.mcpServer.Stop()
	a.mcpServer = nil
	return err
}

// GetMCPStatus returns the current MCP server status.
func (a *App) GetMCPStatus() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.mcpServer != nil {
		return map[string]any{
			"running": true,
			"url":     fmt.Sprintf("http://%s/sse", a.mcpServer.Addr()),
		}
	}
	return map[string]any{
		"running": false,
		"url":     "",
	}
}

// --- Helpers ---

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	if strings.HasPrefix(name, "_vibe_") {
		return false
	}
	return true
}

// ListReports returns every stored banded-report definition as a lightweight
// {id, title, source} summary for the Report View picker (plan §3.4 / P3.10).
func (a *App) ListReports() ([]reports.ReportSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	store := reports.NewReportStore(a.container.SQLiteDB())
	summaries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list reports: %w", err)
	}
	return summaries, nil
}

// RenderReportContent loads a banded report spec via the ReportStore, runs its
// bound query on the real engine, computes the band tree, and renders banded
// HTML (plan §3.3 / P3.8).
func (a *App) RenderReportContent(reportID string) (string, error) {
	return a.RenderReportContentFiltered(reportID, nil)
}

// reportFilterCol matches a safe identifier (column name) for live filtering.
var reportFilterCol = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// filteredSource wraps a report's bound query in a subquery and applies the
// supplied column=value equality filters as a WHERE clause. Filter keys are
// validated as plain identifiers and values are passed as bound parameters, so
// the live Report View filters cannot inject SQL (plan §3.4 / mockup #11 "Live
// DuckDB filters").
func filteredSource(source string, filters map[string]string) (string, []any) {
	var clauses []string
	var args []any
	// Stable ordering for deterministic SQL (and tests).
	keys := make([]string, 0, len(filters))
	for k, v := range filters {
		if v == "" || !reportFilterCol.MatchString(k) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		// Engine binds $name placeholders (see engine.BindParams); namedArgs
		// produces matching p1, p2, … keys.
		clauses = append(clauses, fmt.Sprintf("%q = $p%d", k, i+1))
		args = append(args, filters[k])
	}
	if len(clauses) == 0 {
		return source, nil
	}
	wrapped := fmt.Sprintf("SELECT * FROM (%s) AS _vibe_report_src WHERE %s",
		source, strings.Join(clauses, " AND "))
	return wrapped, args
}

// RenderReportContentFiltered renders the banded report HTML after applying the
// given live equality filters (column=value) to the report's bound query. An
// empty/nil filter map renders the full report (plan §3.4 / mockup #11).
func (a *App) RenderReportContentFiltered(reportID string, filters map[string]string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if a.engine == nil {
		return "", fmt.Errorf("no engine available")
	}

	store := reports.NewReportStore(a.container.SQLiteDB())
	spec, err := store.Get(reportID)
	if err != nil {
		return "", fmt.Errorf("failed to load report %q: %w", reportID, err)
	}
	if spec == nil {
		return "", fmt.Errorf("report %q not found", reportID)
	}

	query, args := filteredSource(spec.Source, filters)
	rs, err := a.engine.Execute(a.ctx, query, namedArgs(args))
	if err != nil {
		return "", fmt.Errorf("failed to run report query for %q: %w", reportID, err)
	}

	tree, err := reports.ComputeBands(rs, *spec)
	if err != nil {
		return "", fmt.Errorf("failed to compute report bands for %q: %w", reportID, err)
	}

	htmlOut, err := a.renderer.RenderReport(tree, *spec, htmlrenderer.RenderReportOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to render report %q: %w", reportID, err)
	}
	return htmlOut, nil
}

// namedArgs converts positional WHERE args into the param map shape the engine
// expects for "?" placeholders (p1, p2, …). Returns nil for no args.
func namedArgs(args []any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	m := make(map[string]any, len(args))
	for i, v := range args {
		m[fmt.Sprintf("p%d", i+1)] = v
	}
	return m
}

// ReportData is a tabular view of a report's bound query result, used by the
// Report View chart/table toggle and to populate live-filter option lists.
type ReportData struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// GetReportData runs the report's bound query (with optional live filters) and
// returns its raw tabular result, so the Report View can render either a chart
// or a data table from the same data and derive filter options (plan §3.4 /
// mockup #11). Studio-only "Edit SQL" reads the source via GetReportSQL.
func (a *App) GetReportData(reportID string, filters map[string]string) (*ReportData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if a.engine == nil {
		return nil, fmt.Errorf("no engine available")
	}

	source, err := a.reportSourceLocked(reportID)
	if err != nil {
		return nil, err
	}

	query, args := filteredSource(source, filters)
	rs, err := a.engine.Execute(a.ctx, query, namedArgs(args))
	if err != nil {
		return nil, fmt.Errorf("failed to run report query for %q: %w", reportID, err)
	}

	out := &ReportData{Columns: make([]string, 0, len(rs.Columns)), Rows: rs.Rows}
	for _, c := range rs.Columns {
		out.Columns = append(out.Columns, c.Name)
	}
	if out.Rows == nil {
		out.Rows = [][]any{}
	}
	return out, nil
}

// GetReportSQL returns the report's bound query source SQL, backing the
// Studio-only "Edit SQL" affordance in the Report View (plan §3.4 / mockup #11).
func (a *App) GetReportSQL(reportID string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.reportSourceLocked(reportID)
}

// reportSourceLocked is the lock-free body shared by reportSource/GetReportSQL.
func (a *App) reportSourceLocked(reportID string) (string, error) {
	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	store := reports.NewReportStore(a.container.SQLiteDB())
	spec, err := store.Get(reportID)
	if err != nil {
		return "", fmt.Errorf("failed to load report %q: %w", reportID, err)
	}
	if spec == nil {
		return "", fmt.Errorf("report %q not found", reportID)
	}
	if strings.TrimSpace(spec.Source) == "" {
		return "", fmt.Errorf("report %q has no bound query", reportID)
	}
	return spec.Source, nil
}

// ExportReportHTML renders the banded report HTML (P3.8) and writes it to dst,
// returning the written path (plan §3.4 / P3.9).
func (a *App) ExportReportHTML(reportID, dst string) (string, error) {
	htmlOut, err := a.RenderReportContent(reportID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(htmlOut), 0o644); err != nil {
		return "", fmt.Errorf("failed to write report HTML to %q: %w", dst, err)
	}
	a.logger.Info("exported report HTML", "report", reportID, "path", dst)
	return dst, nil
}

// reportSource loads a report spec and returns its bound query source SQL.
// Caller must hold a.mu (it is invoked under RLock by ExportReportXLSX).
func (a *App) reportSource(reportID string) (string, error) {
	return a.reportSourceLocked(reportID)
}

// ExportReportXLSX writes the report's bound query result to an .xlsx via the
// DuckDB excel extension (CSV fallback if unavailable), returning the written
// path (plan §3.4 / P3.9).
func (a *App) ExportReportXLSX(reportID, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	source, err := a.reportSource(reportID)
	if err != nil {
		return "", err
	}
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	// Best-effort load of the excel extension (bundled/autoloadable in this
	// build). If it is unavailable we fall back to CSV.
	_, _ = ddb.ExecContext(a.ctx, "LOAD excel")

	// COPY (query) TO 'file.xlsx' WITH (FORMAT xlsx). Escape single quotes in the
	// destination path to keep the SQL literal well-formed.
	xlsxPath := dst
	if !strings.HasSuffix(strings.ToLower(xlsxPath), ".xlsx") {
		xlsxPath += ".xlsx"
	}
	copyXLSX := fmt.Sprintf("COPY (%s) TO '%s' WITH (FORMAT xlsx, HEADER true)",
		source, strings.ReplaceAll(xlsxPath, "'", "''"))
	if _, err := ddb.ExecContext(a.ctx, copyXLSX); err == nil {
		a.logger.Info("exported report XLSX", "report", reportID, "path", xlsxPath)
		return xlsxPath, nil
	} else {
		a.logger.Warn("xlsx export unavailable, falling back to CSV",
			"report", reportID, "error", err)
	}

	// CSV fallback.
	csvPath := strings.TrimSuffix(dst, ".xlsx") + ".csv"
	copyCSV := fmt.Sprintf("COPY (%s) TO '%s' WITH (FORMAT csv, HEADER true)",
		source, strings.ReplaceAll(csvPath, "'", "''"))
	if _, err := ddb.ExecContext(a.ctx, copyCSV); err != nil {
		return "", fmt.Errorf("failed to export report %q to spreadsheet: %w", reportID, err)
	}
	a.logger.Info("exported report CSV (xlsx fallback)", "report", reportID, "path", csvPath)
	return csvPath, nil
}

// AnalyzeInExcel is the one-click "Analyze in Excel/Sheets" alias for
// ExportReportXLSX (plan §3.4 / P3.9).
func (a *App) AnalyzeInExcel(reportID, dst string) (string, error) {
	return a.ExportReportXLSX(reportID, dst)
}

// ExportReportPDF renders the banded report HTML and prints it to a PDF at dst
// via a headless-print backend, returning the written path; degrades gracefully
// with a clear error if no backend is available (plan §3.4 / P3.9).
func (a *App) ExportReportPDF(reportID, dst string) (string, error) {
	htmlOut, err := a.RenderReportContent(reportID)
	if err != nil {
		return "", err
	}

	backend := detectPrintBackend()
	if backend == "" {
		return "", fmt.Errorf("no headless-print backend available: install a Chromium/Chrome browser to enable PDF export")
	}

	// Write the HTML to a temp file and drive the headless browser to print it.
	tmp, err := os.CreateTemp("", "vibe-report-*.html")
	if err != nil {
		return "", fmt.Errorf("failed to create temp HTML for PDF export: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(htmlOut); err != nil {
		tmp.Close()
		return "", fmt.Errorf("failed to write temp HTML for PDF export: %w", err)
	}
	tmp.Close()

	cmd := exec.CommandContext(a.ctx, backend,
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--print-to-pdf="+dst,
		"file://"+tmpPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("headless print failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(dst); err != nil {
		return "", fmt.Errorf("headless print produced no PDF at %q: %w", dst, err)
	}
	a.logger.Info("exported report PDF", "report", reportID, "path", dst, "backend", backend)
	return dst, nil
}

// detectPrintBackend returns the path to a headless-print-capable Chromium/Chrome
// binary, or "" if none is available.
func detectPrintBackend() string {
	for _, name := range []string{
		"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}
