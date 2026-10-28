package api

import "time"

// ProblemListItem is the shape returned by GET /api/v1/problems.
type ProblemListItem struct {
	ID         string    `json:"id" validate:"required"`
	Slug       string    `json:"slug" validate:"required"`
	Title      string    `json:"title" validate:"required"`
	Difficulty string    `json:"difficulty" validate:"required"`
	CreatedAt  time.Time `json:"created_at" validate:"required"`
}

// ProblemDetail is the shape returned by GET /api/v1/problems/{slug}.
type ProblemDetail struct {
	ID          string     `json:"id" validate:"required"`
	Slug        string     `json:"slug" validate:"required"`
	Title       string     `json:"title" validate:"required"`
	Description string     `json:"description" validate:"required"`
	Difficulty  string     `json:"difficulty" validate:"required"`
	CreatedAt   time.Time  `json:"created_at" validate:"required"`
	UpdatedAt   time.Time  `json:"updated_at" validate:"required"`
	TestCases   []TestCase `json:"test_cases" validate:"required"`
}

// TestCase is a public test case returned in problem detail.
type TestCase struct {
	ID             string `json:"id" validate:"required"`
	Name           string `json:"name" validate:"required"`
	Input          string `json:"input" validate:"required"`
	ExpectedOutput string `json:"expected_output" validate:"required"`
}

// Language is the shape returned by GET /api/v1/languages.
type Language struct {
	ID             string `json:"id" validate:"required"`
	DisplayName    string `json:"display_name" validate:"required"`
	MemoryLimitMb  int32  `json:"memory_limit_mb" validate:"required"`
	TimeoutSeconds int32  `json:"timeout_seconds" validate:"required"`
}

// ListProblemsResponse is the envelope for the problem list endpoint.
type ListProblemsResponse struct {
	Data []ProblemListItem `json:"data" validate:"required"`
}

// ListLanguagesResponse is the envelope for the languages list endpoint.
type ListLanguagesResponse struct {
	Data []Language `json:"data" validate:"required"`
}

// CreateSubmissionRequest is the body of POST /api/v1/submissions.
type CreateSubmissionRequest struct {
	ProblemID  string `json:"problem_id" validate:"required"`
	LanguageID string `json:"language_id" validate:"required"`
	SourceCode string `json:"source_code" validate:"required"`
}

// CreateSubmissionResponse is the body returned on a successful submission.
type CreateSubmissionResponse struct {
	SubmissionID string `json:"submission_id" validate:"required"`
}

// ErrorResponse is the documented error response shape.
type ErrorResponse struct {
	Error ErrorDetail `json:"error" validate:"required"`
}

// ErrorDetail is the inner error object.
type ErrorDetail struct {
	Code    string `json:"code" validate:"required"`
	Message string `json:"message" validate:"required"`
}

// HealthResponse is the response shape for health and readiness endpoints.
type HealthResponse struct {
	Status string `json:"status" validate:"required"`
}
