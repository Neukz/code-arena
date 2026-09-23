package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorEnvelope is the documented error response shape.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody holds a machine-readable code and human-readable message.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("failed to encode json response", slog.String("error", err.Error()))
	}
}
