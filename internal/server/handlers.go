package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

const defaultPageSize = 50

// handleIndex lists all pages in the container.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil {
		s.renderError(w, http.StatusServiceUnavailable, "no container loaded")
		return
	}

	db := s.container.SQLiteDB()

	rows, err := db.QueryContext(r.Context(),
		"SELECT id, title, page_type, sort_order FROM _vibe_pages ORDER BY sort_order, title;")
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list pages: %v", err))
		return
	}
	defer rows.Close()

	type pageEntry struct {
		ID        string
		Title     string
		PageType  string
		SortOrder int
	}

	var pages []pageEntry
	for rows.Next() {
		var p pageEntry
		if err := rows.Scan(&p.ID, &p.Title, &p.PageType, &p.SortOrder); err != nil {
			s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan page: %v", err))
			return
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to iterate pages: %v", err))
		return
	}

	// Also list user tables from DuckDB.
	var tables []string
	if duckDB := s.container.DuckDBDB(); duckDB != nil {
		tableRows, err := duckDB.QueryContext(r.Context(),
			"SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name;")
		if err != nil {
			s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list tables: %v", err))
			return
		}
		defer tableRows.Close()

		for tableRows.Next() {
			var name string
			if err := tableRows.Scan(&name); err != nil {
				s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan table: %v", err))
				return
			}
			tables = append(tables, name)
		}
		if err := tableRows.Err(); err != nil {
			s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to iterate tables: %v", err))
			return
		}
	}

	data := map[string]any{
		"Pages":  pages,
		"Tables": tables,
	}

	s.renderTemplate(w, "index.html", data)
}

// handlePage renders a single page as HTML.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if pageID == "" {
		http.NotFound(w, r)
		return
	}

	if s.renderer == nil {
		s.renderError(w, http.StatusServiceUnavailable, "renderer not available")
		return
	}

	// Collect query params for page rendering.
	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	html, err := s.renderer.RenderPageHTML(pageID, params)
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to render page: %v", err))
		return
	}

	data := map[string]any{
		"PageID":  pageID,
		"Content": template.HTML(html), //nolint:gosec // rendered page HTML is trusted
	}

	s.renderTemplate(w, "page.html", data)
}

// handleFormSubmit processes a form POST submission.
func (s *Server) handleFormSubmit(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	formID := r.PathValue("formID")
	if pageID == "" || formID == "" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil || s.engine == nil {
		s.renderError(w, http.StatusServiceUnavailable, "container or engine not available")
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse form: %v", err))
		return
	}

	// Collect form values as parameters.
	params := make(map[string]any)
	for k, v := range r.PostForm {
		if len(v) == 1 {
			params[k] = v[0]
		} else {
			params[k] = v
		}
	}

	// Look up the form's SQL from _vibe_components.
	db := s.container.SQLiteDB()
	var config string
	err := db.QueryRowContext(r.Context(),
		"SELECT config FROM _vibe_components WHERE id = ? AND type = 'form';", formID).Scan(&config)
	if err != nil {
		s.renderError(w, http.StatusNotFound, fmt.Sprintf("form %q not found: %v", formID, err))
		return
	}

	// Parse form config to get the SQL statement.
	var formConfig struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal([]byte(config), &formConfig); err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse form config: %v", err))
		return
	}

	_, err = s.engine.Execute(r.Context(), formConfig.SQL, params)
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to execute form query: %v", err))
		return
	}

	// Broadcast reload event after data modification.
	s.sse.Broadcast("reload")

	// Redirect back to the page.
	http.Redirect(w, r, "/page/"+pageID, http.StatusSeeOther)
}

// handleTableBrowser renders a paginated table view.
func (s *Server) handleTableBrowser(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("name")
	if tableName == "" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil {
		s.renderError(w, http.StatusServiceUnavailable, "no container loaded")
		return
	}

	// Parse pagination params.
	page := 1
	pageSize := defaultPageSize
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 1000 {
			pageSize = v
		}
	}

	offset := (page - 1) * pageSize
	db := s.container.DuckDBDB()
	if db == nil {
		s.renderError(w, http.StatusServiceUnavailable, "DuckDB not available")
		return
	}

	// Sanitize table name to prevent SQL injection (only allow alphanumeric and underscore).
	if !isValidIdentifier(tableName) {
		s.renderError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	// Get total row count.
	var totalRows int
	err := db.QueryRowContext(r.Context(),
		fmt.Sprintf("SELECT COUNT(*) FROM %q;", tableName)).Scan(&totalRows) //nolint:gosec
	if err != nil {
		s.renderError(w, http.StatusNotFound, fmt.Sprintf("table %q not found: %v", tableName, err))
		return
	}

	// Fetch page of data.
	rows, err := db.QueryContext(r.Context(),
		fmt.Sprintf("SELECT * FROM %q LIMIT ? OFFSET ?;", tableName), pageSize, offset) //nolint:gosec
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query table: %v", err))
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get columns: %v", err))
		return
	}

	var data [][]string
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan row: %v", err))
			return
		}

		row := make([]string, len(columns))
		for i, v := range values {
			row[i] = fmt.Sprintf("%v", v)
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		s.renderError(w, http.StatusInternalServerError, fmt.Sprintf("failed to iterate rows: %v", err))
		return
	}

	totalPages := (totalRows + pageSize - 1) / pageSize

	tplData := map[string]any{
		"TableName":  tableName,
		"Columns":    columns,
		"Rows":       data,
		"Page":       page,
		"PageSize":   pageSize,
		"TotalRows":  totalRows,
		"TotalPages": totalPages,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
	}

	s.renderTemplate(w, "table_browser.html", tplData)
}

// handleTableJSON exports a table as JSON.
func (s *Server) handleTableJSON(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("name")
	if tableName == "" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "no container loaded")
		return
	}

	if !isValidIdentifier(tableName) {
		writeJSONError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	db := s.container.DuckDBDB()
	if db == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "DuckDB not available")
		return
	}
	rows, err := db.QueryContext(r.Context(),
		fmt.Sprintf("SELECT * FROM %q;", tableName)) //nolint:gosec
	if err != nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("table %q not found: %v", tableName, err))
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get columns: %v", err))
		return
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan row: %v", err))
			return
		}

		row := make(map[string]any)
		for i, col := range columns {
			row[col] = values[i]
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to iterate rows: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", tableName+".json"))

	if err := json.NewEncoder(w).Encode(result); err != nil {
		s.logger.Error("failed to encode JSON response", "error", err)
	}
}

// handleTableCSV exports a table as CSV.
func (s *Server) handleTableCSV(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("name")
	if tableName == "" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil {
		http.Error(w, "no container loaded", http.StatusServiceUnavailable)
		return
	}

	if !isValidIdentifier(tableName) {
		http.Error(w, "invalid table name", http.StatusBadRequest)
		return
	}

	db := s.container.DuckDBDB()
	if db == nil {
		http.Error(w, "DuckDB not available", http.StatusServiceUnavailable)
		return
	}
	rows, err := db.QueryContext(r.Context(),
		fmt.Sprintf("SELECT * FROM %q;", tableName)) //nolint:gosec
	if err != nil {
		http.Error(w, fmt.Sprintf("table %q not found: %v", tableName, err), http.StatusNotFound)
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get columns: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", tableName+".csv"))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header row.
	if err := writer.Write(columns); err != nil {
		s.logger.Error("failed to write CSV header", "error", err)
		return
	}

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			s.logger.Error("failed to scan row for CSV", "error", err)
			return
		}

		row := make([]string, len(columns))
		for i, v := range values {
			row[i] = fmt.Sprintf("%v", v)
		}

		if err := writer.Write(row); err != nil {
			s.logger.Error("failed to write CSV row", "error", err)
			return
		}
	}
}

// handleQuery executes an ad-hoc SQL query and returns JSON results.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if !s.allowSQL {
		writeJSONError(w, http.StatusForbidden, "ad-hoc SQL queries are disabled")
		return
	}

	if s.engine == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "engine not available")
		return
	}

	var req struct {
		SQL    string         `json:"sql"`
		Params map[string]any `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request: %v", err))
		return
	}

	if req.SQL == "" {
		writeJSONError(w, http.StatusBadRequest, "sql field is required")
		return
	}

	rs, err := s.engine.Execute(r.Context(), req.SQL, req.Params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	// Convert to JSON-friendly format.
	columns := make([]map[string]string, len(rs.Columns))
	for i, col := range rs.Columns {
		columns[i] = map[string]string{
			"name": col.Name,
			"type": col.Type,
		}
	}

	resp := map[string]any{
		"columns": columns,
		"rows":    rs.Rows,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode query response", "error", err)
	}
}

// handleSchema returns schema introspection data as JSON.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	if s.container == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "no container loaded")
		return
	}

	db := s.container.DuckDBDB()
	if db == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "DuckDB not available")
		return
	}

	// Get all user tables and their columns from DuckDB.
	tableRows, err := db.QueryContext(r.Context(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name;")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list tables: %v", err))
		return
	}
	defer tableRows.Close()

	type columnInfo struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		NotNull bool   `json:"not_null"`
		PK      bool   `json:"pk"`
	}

	type tableInfo struct {
		Name    string       `json:"name"`
		Columns []columnInfo `json:"columns"`
	}

	var tables []tableInfo
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan table: %v", err))
			return
		}

		// Get column info from information_schema.
		colRows, err := db.QueryContext(r.Context(),
			"SELECT column_name, data_type, CASE WHEN is_nullable = 'NO' THEN true ELSE false END AS not_null FROM information_schema.columns WHERE table_schema = 'main' AND table_name = $1 ORDER BY ordinal_position",
			name)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get table info: %v", err))
			return
		}

		var cols []columnInfo
		for colRows.Next() {
			var colName, colType string
			var notNull bool
			if err := colRows.Scan(&colName, &colType, &notNull); err != nil {
				colRows.Close()
				writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan column: %v", err))
				return
			}
			cols = append(cols, columnInfo{
				Name:    colName,
				Type:    colType,
				NotNull: notNull,
				PK:      false,
			})
		}
		colRows.Close()

		tables = append(tables, tableInfo{
			Name:    name,
			Columns: cols,
		})
	}

	if err := tableRows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to iterate tables: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"tables": tables}); err != nil {
		s.logger.Error("failed to encode schema response", "error", err)
	}
}

// handleAsset serves a binary asset from the container.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("id")
	if assetID == "" {
		http.NotFound(w, r)
		return
	}

	if s.container == nil {
		http.Error(w, "no container loaded", http.StatusServiceUnavailable)
		return
	}

	db := s.container.SQLiteDB()

	var mimeType string
	var data []byte
	var filename string

	err := db.QueryRowContext(r.Context(),
		"SELECT filename, mime_type, data FROM _vibe_assets WHERE id = ?;", assetID).
		Scan(&filename, &mimeType, &data)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	if filename != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data) //nolint:errcheck
}

// renderTemplate renders a named template with the given data.
func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	tmpl, err := template.New("base.html").ParseFS(templateFS, "templates/base.html", "templates/"+name)
	if err != nil {
		s.logger.Error("failed to parse template", "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Error("failed to execute template", "name", name, "error", err)
	}
}

// renderError renders an error page with the given status code and message.
func (s *Server) renderError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)

	tmpl, err := template.New("base.html").ParseFS(templateFS, "templates/base.html")
	if err != nil {
		http.Error(w, msg, code)
		return
	}

	data := map[string]any{
		"Error":      msg,
		"StatusCode": code,
	}
	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Error("failed to render error template", "error", err)
	}
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// isValidIdentifier checks that a string is safe to use as a SQL identifier.
func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	// Must not start with a digit.
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	// Reject _vibe_ prefix for security.
	if strings.HasPrefix(name, "_vibe_") {
		return false
	}
	return true
}
