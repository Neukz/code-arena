package api

import "net/http"

// Routes wires all API endpoints and applies shared middleware.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	return withMiddleware(mux, h.log)
}
