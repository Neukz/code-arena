package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Handler holds HTTP handlers and their dependencies.
type Handler struct {
	store Querier
	log   *slog.Logger
}

// NewHandler creates a handler group backed by the given store and logger.
func NewHandler(store Querier, log *slog.Logger) *Handler {
	return &Handler{store: store, log: log}
}

// errorResponse is the documented error response shape.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.log.Error("failed to encode json response", slog.String("error", err.Error()))
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	body := errorResponse{}
	body.Error.Code = code
	body.Error.Message = message
	h.writeJSON(w, status, body)
}

type healthResponse struct {
	Status string `json:"status"`
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if _, err := h.store.Ping(ctx); err != nil {
		h.log.Error("readyz db ping failed", slog.String("error", err.Error()))
		h.writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable"})
		return
	}

	h.writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
}

func (h *Handler) listProblems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	problems, err := h.store.ListProblems(ctx, db.NullProblemDifficulty{})
	if err != nil {
		h.log.Error("failed to list problems", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to list problems")
		return
	}

	resp := ListProblemsResponse{Data: make([]ProblemListItem, 0, len(problems))}
	for _, p := range problems {
		resp.Data = append(resp.Data, ProblemListItem{
			ID:         pgUUIDToString(p.ID),
			Slug:       p.Slug,
			Title:      p.Title,
			Difficulty: string(p.Difficulty),
			CreatedAt:  pgTimestamptzToTime(p.CreatedAt),
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getProblem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
	if slug == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "missing problem slug")
		return
	}

	problem, err := h.store.GetProblemBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeError(w, http.StatusNotFound, "not_found", "problem not found")
			return
		}
		h.log.Error("failed to get problem", slog.String("slug", slug), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to get problem")
		return
	}

	testCases, err := h.store.GetSampleTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		h.log.Error("failed to get sample test cases", slog.String("slug", slug), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to get problem")
		return
	}

	resp := ProblemDetail{
		ID:          pgUUIDToString(problem.ID),
		Slug:        problem.Slug,
		Title:       problem.Title,
		Description: problem.Description,
		Difficulty:  string(problem.Difficulty),
		CreatedAt:   pgTimestamptzToTime(problem.CreatedAt),
		UpdatedAt:   pgTimestamptzToTime(problem.UpdatedAt),
		TestCases:   make([]TestCase, 0, len(testCases)),
	}
	for _, tc := range testCases {
		resp.TestCases = append(resp.TestCases, TestCase{
			ID:             pgUUIDToString(tc.ID),
			Name:           tc.Name,
			Input:          tc.Input,
			ExpectedOutput: tc.ExpectedOutput,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listLanguages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	langs, err := h.store.ListLanguages(ctx)
	if err != nil {
		h.log.Error("failed to list languages", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to list languages")
		return
	}

	resp := ListLanguagesResponse{Data: make([]Language, 0, len(langs))}
	for _, l := range langs {
		resp.Data = append(resp.Data, Language{
			ID:             l.ID,
			DisplayName:    l.DisplayName,
			MemoryLimitMb:  l.MemoryLimitMb,
			TimeoutSeconds: l.TimeoutSeconds,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// pgUUIDToString converts a pgtype.UUID to its canonical string representation.
// Invalid UUIDs return an empty string.
func pgUUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}

// pgTimestamptzToTime converts a pgtype.Timestamptz to time.Time.
// Invalid timestamps return the zero time.
func pgTimestamptzToTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}
