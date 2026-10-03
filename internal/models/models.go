package models

// SubmissionJob is the message published to the queue for each submission.
type SubmissionJob struct {
	SubmissionID string `json:"submission_id"`
	ProblemID    string `json:"problem_id"`
	LanguageID   string `json:"language_id"`
	SourceCode   string `json:"source_code"`
}
