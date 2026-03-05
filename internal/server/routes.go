package server

import (
	"net/http"
)

// setupRoutes creates the HTTP handler with all routes and middleware.
func (s *Server) setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Page routes.
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /page/{id}", s.handlePage)
	mux.HandleFunc("POST /page/{id}/submit/{formID}", s.handleFormSubmit)

	// Table routes.
	// Note: Go ServeMux doesn't support {name}.json patterns, so we use
	// /table/export/{format}/{name} for exports.
	mux.HandleFunc("GET /table/{name}", s.handleTableBrowser)
	mux.HandleFunc("GET /table/export/json/{name}", s.handleTableJSON)
	mux.HandleFunc("GET /table/export/csv/{name}", s.handleTableCSV)

	// API routes.
	mux.HandleFunc("POST /api/query", s.handleQuery)
	mux.HandleFunc("GET /api/schema", s.handleSchema)

	// SSE live reload.
	mux.Handle("GET /events", s.sse)

	// Asset routes.
	mux.HandleFunc("GET /assets/{id}", s.handleAsset)

	// Static files.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Apply middleware (outermost first).
	var handler http.Handler = mux
	handler = logging(s.logger)(handler)
	handler = recovery(s.logger)(handler)
	handler = localhostOnly(handler)

	return handler
}
