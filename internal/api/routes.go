package api

import "net/http"

// Routes wires all API endpoints and applies shared middleware.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /api/v1/problems", h.listProblems)
	mux.HandleFunc("GET /api/v1/problems/{slug}", h.getProblem)
	mux.HandleFunc("GET /api/v1/languages", h.listLanguages)
	mux.HandleFunc("POST /api/v1/submissions", h.createSubmission)

	return withMiddleware(mux, h.log, h.corsAllowedOrigins)
}
