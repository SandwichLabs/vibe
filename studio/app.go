package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	ctx       context.Context
	logger    *slog.Logger
	mu        sync.RWMutex
	container *container.Container
	engine    *engine.Manager
	registry  *queries.Registry
	renderer  *htmlrenderer.Renderer
	filePath  string
	mcpServer *vibemcp.Server
}

// NewApp creates a new App instance.
func NewApp(logger *slog.Logger) *App {
	return &App{
		logger:   logger,
		renderer: htmlrenderer.NewRenderer(logger),
	}
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
	rows, err := db.Query("SELECT id, title, page_type, sort_order FROM _vibe_pages ORDER BY sort_order, title")
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

// RenderPageContent renders a page's content as HTML fragments.
func (a *App) RenderPageContent(id string, params map[string]string) (string, error) {
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

	executed, err := pages.Execute(context.Background(), page, &engineAdapter{mgr: a.engine}, engineParams)
	if err != nil {
		return "", fmt.Errorf("failed to execute page: %w", err)
	}

	return a.renderExecutedNodes(executed, pageType)
}

// renderExecutedNodes renders executed page nodes to HTML fragments.
func (a *App) renderExecutedNodes(executed *pages.ExecutedPage, pageType string) (string, error) {
	var b strings.Builder

	for _, en := range executed.Nodes {
		switch n := en.Node.(type) {
		case *pages.TextNode:
			if pageType == "html" {
				b.WriteString(n.Content)
			} else {
				var mdBuf bytes.Buffer
				if err := goldmark.Convert([]byte(n.Content), &mdBuf); err != nil {
					b.WriteString("<div class=\"prose\">" + n.Content + "</div>\n")
				} else {
					b.WriteString("<div class=\"prose\">" + mdBuf.String() + "</div>\n")
				}
			}

		case *pages.SQLBlockNode:
			// SQL blocks don't render directly (they produce result sets for directives).

		case *pages.DirectiveNode:
			html, err := a.renderDirective(n, en.Result)
			if err != nil {
				b.WriteString(fmt.Sprintf("<div class=\"error\">Error rendering %s: %s</div>\n", n.Type, err))
				continue
			}
			b.WriteString(html)
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}

// renderDirective renders a single directive node to HTML.
func (a *App) renderDirective(node *pages.DirectiveNode, result *pages.ResultSet) (string, error) {
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
			Source:    getStringConfig(node.Config, "source"),
			Sortable: true,
			ColumnFmt: getStringMapConfig(node.Config, "fmt"),
			Result:   rrs,
		})

	case "chart":
		return a.renderer.RenderChart(&renderer.ChartConfig{
			Source: getStringConfig(node.Config, "source"),
			Type:   getStringConfig(node.Config, "type"),
			X:      getStringConfig(node.Config, "x"),
			Y:      getStringConfig(node.Config, "y"),
			Title:  getStringConfig(node.Config, "title"),
			Color:  getStringConfig(node.Config, "color"),
			Result: rrs,
		})

	case "metric":
		var value any
		if rrs != nil && len(rrs.Rows) > 0 && len(rrs.Rows[0]) > 0 {
			value = rrs.Rows[0][0]
		}
		// Support both "fmt" key and legacy "format" key
		fmtCode := getStringConfig(node.Config, "fmt")
		return a.renderer.RenderMetric(&renderer.MetricConfig{
			Label:  getStringConfig(node.Config, "label"),
			Format: getStringConfig(node.Config, "format"),
			Fmt:    fmtCode,
			Value:  value,
		})

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

	default:
		return fmt.Sprintf("<div>Unknown directive: %s</div>", node.Type), nil
	}
}

func getStringConfig(config map[string]any, key string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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
	Columns    []string   `json:"columns"`
	Rows       [][]any    `json:"rows"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalRows  int        `json:"total_rows"`
	TotalPages int        `json:"total_pages"`
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

// InsertRow inserts a row into a table.
func (a *App) InsertRow(table string, values map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name")
	}

	cols := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for k, v := range values {
		cols = append(cols, fmt.Sprintf("%q", k))
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}

	query := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to insert row: %w", err)
	}
	return nil
}

// UpdateRow updates a row in a table.
func (a *App) UpdateRow(table, pkCol string, pkVal any, values map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name")
	}

	setClauses := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+1)
	for k, v := range values {
		setClauses = append(setClauses, fmt.Sprintf("%q = ?", k))
		args = append(args, v)
	}
	args = append(args, pkVal)

	query := fmt.Sprintf("UPDATE %q SET %s WHERE %q = ?", table, strings.Join(setClauses, ", "), pkCol)
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update row: %w", err)
	}
	return nil
}

// DeleteRow deletes a row from a table.
func (a *App) DeleteRow(table, pkCol string, pkVal any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name")
	}

	query := fmt.Sprintf("DELETE FROM %q WHERE %q = ?", table, pkCol)
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(query, args(pkVal)...)
	if err != nil {
		return fmt.Errorf("failed to delete row: %w", err)
	}
	return nil
}

func args(vals ...any) []any { return vals }

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
	SQL      string  `json:"sql,omitempty"`
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
