package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	maxSourceCodeLength = 64 * 1024
	publishTimeout      = 5 * time.Second
)

// Handler holds HTTP handlers and their dependencies.
type Handler struct {
	store     Querier
	publisher queue.JobPublisher
	log       *slog.Logger
}

// NewHandler creates a handler group backed by the given store, publisher, and logger.
func NewHandler(store Querier, publisher queue.JobPublisher, log *slog.Logger) *Handler {
	return &Handler{store: store, publisher: publisher, log: log}
}

// ErrorResponse is the documented error response shape.
type ErrorResponse struct {
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
	body := ErrorResponse{}
	body.Error.Code = code
	body.Error.Message = message
	h.writeJSON(w, status, body)
}

// HealthResponse is the response shape for health and readiness endpoints.
type HealthResponse struct {
	Status string `json:"status"`
}

// healthz godoc
//
// @Summary     Health check
// @Description Returns ok if the service is running.
// @Tags        health
// @Produce     json
// @Success     200 {object} HealthResponse
// @Router      /healthz [get]
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// readyz godoc
//
// @Summary     Readiness check
// @Description Returns ready when the database is reachable; otherwise unavailable.
// @Tags        health
// @Produce     json
// @Success     200 {object} HealthResponse
// @Failure     503 {object} HealthResponse
// @Router      /readyz [get]
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if _, err := h.store.Ping(ctx); err != nil {
		h.log.Error("readyz db ping failed", slog.String("error", err.Error()))
		h.writeJSON(w, http.StatusServiceUnavailable, HealthResponse{Status: "unavailable"})
		return
	}

	h.writeJSON(w, http.StatusOK, HealthResponse{Status: "ready"})
}

// listProblems godoc
//
// @Summary     List problems
// @Description Returns all problems with id, slug, title, and difficulty.
// @Tags        problems
// @Produce     json
// @Success     200 {object} ListProblemsResponse
// @Failure     500 {object} ErrorResponse
// @Router      /api/v1/problems [get]
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

// getProblem godoc
//
// @Summary     Get problem by slug
// @Description Returns problem details including the Markdown prompt and sample test cases.
// @Tags        problems
// @Produce     json
// @Param       slug path string true "Problem slug"
// @Success     200 {object} ProblemDetail
// @Failure     400 {object} ErrorResponse
// @Failure     404 {object} ErrorResponse
// @Failure     500 {object} ErrorResponse
// @Router      /api/v1/problems/{slug} [get]
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

// listLanguages godoc
//
// @Summary		List supported languages
// @Description	Returns all supported programming languages with their resource limits.
// @Tags        languages
// @Produce     json
// @Success     200 {object} ListLanguagesResponse
// @Failure     500 {object} ErrorResponse
// @Router      /api/v1/languages [get]
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

// createSubmission godoc
//
// @Summary     Create a submission
// @Description Validates the request, persists a pending submission, and enqueues it for grading.
// @Tags        submissions
// @Accept      json
// @Produce     json
// @Param       request body CreateSubmissionRequest true "Submission to grade"
// @Success     202 {object} CreateSubmissionResponse
// @Failure     400 {object} ErrorResponse
// @Failure     500 {object} ErrorResponse
// @Failure     503 {object} ErrorResponse
// @Router      /api/v1/submissions [post]
func (h *Handler) createSubmission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			h.log.Error("failed to close request body", slog.String("error", err.Error()))
		}
	}()

	if req.ProblemID == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "problem_id is required")
		return
	}
	problemID, err := parseUUID(req.ProblemID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "problem_id must be a valid uuid")
		return
	}

	if req.LanguageID == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "language_id is required")
		return
	}
	if req.SourceCode == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "source_code is required")
		return
	}
	if len(req.SourceCode) > maxSourceCodeLength {
		h.writeError(w, http.StatusBadRequest, "bad_request", "source_code exceeds maximum length")
		return
	}

	if _, err := h.store.GetProblemByID(ctx, problemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeError(w, http.StatusBadRequest, "bad_request", "problem does not exist")
			return
		}
		h.log.Error("failed to validate problem", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate submission")
		return
	}

	if _, err := h.store.GetLanguageByID(ctx, req.LanguageID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeError(w, http.StatusBadRequest, "bad_request", "language does not exist")
			return
		}
		h.log.Error("failed to validate language", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate submission")
		return
	}

	submission, err := h.store.CreateSubmission(ctx, db.CreateSubmissionParams{
		ProblemID:  problemID,
		LanguageID: req.LanguageID,
		SourceCode: req.SourceCode,
	})
	if err != nil {
		h.log.Error("failed to create submission", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to create submission")
		return
	}

	job := models.SubmissionJob{
		SubmissionID: pgUUIDToString(submission.ID),
		ProblemID:    req.ProblemID,
		LanguageID:   req.LanguageID,
		SourceCode:   req.SourceCode,
	}

	publishCtx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	if err := h.publisher.Publish(publishCtx, job); err != nil {
		h.log.Error("failed to publish submission job", slog.String("error", err.Error()))

		if markErr := h.store.MarkSubmissionFailed(ctx, db.MarkSubmissionFailedParams{
			ID:           submission.ID,
			ErrorMessage: pgtype.Text{String: "failed to enqueue job: " + err.Error(), Valid: true},
		}); markErr != nil {
			h.log.Error("failed to mark submission failed after publish error", slog.String("error", markErr.Error()))
		}

		h.writeError(w, http.StatusServiceUnavailable, "service_unavailable", "failed to enqueue submission")
		return
	}

	h.writeJSON(w, http.StatusAccepted, CreateSubmissionResponse{SubmissionID: pgUUIDToString(submission.ID)})
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

// parseUUID converts a string UUID to pgtype.UUID.
func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}
