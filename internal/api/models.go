package api

import "time"

// ProblemListItem is the shape returned by GET /api/v1/problems.
type ProblemListItem struct {
	ID         string    `json:"id"`
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	Difficulty string    `json:"difficulty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ProblemDetail is the shape returned by GET /api/v1/problems/{slug}.
type ProblemDetail struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Difficulty  string     `json:"difficulty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	TestCases   []TestCase `json:"test_cases"`
}

// TestCase is a public test case returned in problem detail.
type TestCase struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Input          string `json:"input"`
	ExpectedOutput string `json:"expected_output"`
}

// Language is the shape returned by GET /api/v1/languages.
type Language struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	MemoryLimitMb  int32  `json:"memory_limit_mb"`
	TimeoutSeconds int32  `json:"timeout_seconds"`
}

// ListProblemsResponse is the envelope for the problem list endpoint.
type ListProblemsResponse struct {
	Data []ProblemListItem `json:"data"`
}

// ListLanguagesResponse is the envelope for the languages list endpoint.
type ListLanguagesResponse struct {
	Data []Language `json:"data"`
}
