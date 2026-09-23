package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger abstracts a dependency that can be pinged for readiness checks.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler holds HTTP handlers and their dependencies.
type Handler struct {
	db  Pinger
	log *slog.Logger
}

// NewHandler creates a handler group backed by the given pinger and logger.
func NewHandler(db Pinger, log *slog.Logger) *Handler {
	return &Handler{db: db, log: log}
}

type healthResponse struct {
	Status string `json:"status"`
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.log.Error("readyz db ping failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable"})
		return
	}

	writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
}
