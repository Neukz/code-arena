package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/sandbox"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultPidsLimit = 64

// Repository describes the data access operations the worker needs.
type Repository interface {
	GetSubmissionByID(ctx context.Context, id pgtype.UUID) (db.Submission, error)
	MarkSubmissionRunning(ctx context.Context, id pgtype.UUID) error
	GetLanguageByID(ctx context.Context, id string) (db.Language, error)
	GetProblemByID(ctx context.Context, id pgtype.UUID) (db.Problem, error)
	GetTestCasesByProblemID(ctx context.Context, problemID pgtype.UUID) ([]db.TestCase, error)
	UpsertSubmissionResult(ctx context.Context, arg db.UpsertSubmissionResultParams) error
	MarkSubmissionCompleted(ctx context.Context, arg db.MarkSubmissionCompletedParams) error
	MarkSubmissionFailed(ctx context.Context, arg db.MarkSubmissionFailedParams) error
}

// SandboxRunner executes submission code inside isolated containers.
type SandboxRunner interface {
	Run(ctx context.Context, req sandbox.RunRequest) ([]sandbox.TestResult, error)
}

// Processor orchestrates the submission lifecycle from a queue job.
type Processor struct {
	queries Repository
	runner  SandboxRunner
	log     *slog.Logger
}

// NewProcessor creates a processor backed by the given repository and runner.
func NewProcessor(queries Repository, runner SandboxRunner, log *slog.Logger) *Processor {
	return &Processor{
		queries: queries,
		runner:  runner,
		log:     log,
	}
}

// Process consumes a submission job, executes the code against all test cases,
// persists the results, and transitions the submission to completed or failed.
// It is idempotent: a redelivered job for an already-completed submission is a no-op.
func (p *Processor) Process(ctx context.Context, job models.SubmissionJob) error {
	submissionID, err := parseUUID(job.SubmissionID)
	if err != nil {
		return fmt.Errorf("invalid submission_id: %w", err)
	}
	problemID, err := parseUUID(job.ProblemID)
	if err != nil {
		return fmt.Errorf("invalid problem_id: %w", err)
	}

	sub, err := p.queries.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return fmt.Errorf("get submission: %w", err)
	}
	if sub.Status == db.SubmissionStatusCompleted {
		p.log.Info("skipping already completed submission", slog.String("submission_id", job.SubmissionID))
		return nil
	}

	if err := p.queries.MarkSubmissionRunning(ctx, submissionID); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}

	lang, err := p.queries.GetLanguageByID(ctx, job.LanguageID)
	if err != nil {
		return p.markFailed(ctx, submissionID, fmt.Errorf("get language %q: %w", job.LanguageID, err))
	}

	if _, err := p.queries.GetProblemByID(ctx, problemID); err != nil {
		return p.markFailed(ctx, submissionID, fmt.Errorf("get problem: %w", err))
	}

	testCases, err := p.queries.GetTestCasesByProblemID(ctx, problemID)
	if err != nil {
		return p.markFailed(ctx, submissionID, fmt.Errorf("get test cases: %w", err))
	}

	req, err := p.toRunRequest(job.SourceCode, lang, testCases)
	if err != nil {
		return p.markFailed(ctx, submissionID, fmt.Errorf("build run request: %w", err))
	}

	results, err := p.runner.Run(ctx, req)
	if err != nil {
		if errors.Is(err, sandbox.ErrCompileFailed) {
			return p.handleCompileError(ctx, submissionID, testCases, err)
		}
		return p.markFailed(ctx, submissionID, fmt.Errorf("sandbox run: %w", err))
	}

	tcResults, overall, err := evaluateResults(testCases, results)
	if err != nil {
		return p.markFailed(ctx, submissionID, err)
	}

	for _, r := range tcResults {
		if upsertErr := p.queries.UpsertSubmissionResult(ctx, db.UpsertSubmissionResultParams{
			SubmissionID:    submissionID,
			TestCaseID:      r.testCaseID,
			Verdict:         r.verdict,
			Stdout:          pgtype.Text{String: r.stdout, Valid: true},
			Stderr:          pgtype.Text{String: r.stderr, Valid: true},
			ExecutionTimeMs: pgtype.Int4{Int32: durationToMillis(r.duration), Valid: true},
		}); upsertErr != nil {
			return fmt.Errorf("upsert result: %w", upsertErr)
		}
	}

	if err := p.queries.MarkSubmissionCompleted(ctx, db.MarkSubmissionCompletedParams{
		ID:             submissionID,
		OverallVerdict: db.NullVerdict{Verdict: overall, Valid: true},
	}); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	p.log.Info("submission processed",
		slog.String("submission_id", job.SubmissionID),
		slog.String("overall_verdict", string(overall)),
		slog.Int("test_cases", len(testCases)),
	)
	return nil
}

func (p *Processor) markFailed(ctx context.Context, id pgtype.UUID, err error) error {
	if markErr := p.queries.MarkSubmissionFailed(ctx, db.MarkSubmissionFailedParams{
		ID:           id,
		ErrorMessage: pgtype.Text{String: err.Error(), Valid: true},
	}); markErr != nil {
		return fmt.Errorf("mark failed: %w", markErr)
	}
	return err
}

func (p *Processor) handleCompileError(ctx context.Context, submissionID pgtype.UUID, testCases []db.TestCase, err error) error {
	for _, tc := range testCases {
		if upsertErr := p.queries.UpsertSubmissionResult(ctx, db.UpsertSubmissionResultParams{
			SubmissionID: submissionID,
			TestCaseID:   tc.ID,
			Verdict:      db.VerdictCompileError,
			Stderr:       pgtype.Text{String: err.Error(), Valid: true},
		}); upsertErr != nil {
			return fmt.Errorf("upsert compile error result: %w", upsertErr)
		}
	}

	if err := p.queries.MarkSubmissionCompleted(ctx, db.MarkSubmissionCompletedParams{
		ID:             submissionID,
		OverallVerdict: db.NullVerdict{Verdict: db.VerdictCompileError, Valid: true},
	}); err != nil {
		return fmt.Errorf("mark completed after compile error: %w", err)
	}
	return nil
}

func (p *Processor) toRunRequest(sourceCode string, lang db.Language, testCases []db.TestCase) (sandbox.RunRequest, error) {
	sandboxPath, err := sourcePath(lang)
	if err != nil {
		return sandbox.RunRequest{}, err
	}

	var compileCmd []string
	if lang.CompileCmd.Valid {
		compileCmd = strings.Fields(lang.CompileCmd.String)
	}

	runCmd := strings.Fields(lang.RunCmd)
	if len(runCmd) == 0 {
		return sandbox.RunRequest{}, fmt.Errorf("empty run command for language %q", lang.ID)
	}

	sandboxCases := make([]sandbox.TestCase, len(testCases))
	for i, tc := range testCases {
		sandboxCases[i] = sandbox.TestCase{Input: tc.Input}
	}

	return sandbox.RunRequest{
		Image:       lang.DockerImage,
		SandboxPath: sandboxPath,
		CompileCmd:  compileCmd,
		RunCmd:      runCmd,
		SourceCode:  sourceCode,
		TestCases:   sandboxCases,
		Timeout:     time.Duration(lang.TimeoutSeconds) * time.Second,
		Resources: sandbox.Resources{
			MemoryMB:  int(lang.MemoryLimitMb),
			CPU:       cpuCores(lang),
			PidsLimit: defaultPidsLimit,
		},
	}, nil
}

func sourcePath(lang db.Language) (string, error) {
	if lang.CompileCmd.Valid {
		args := strings.Fields(lang.CompileCmd.String)
		if len(args) > 0 {
			return args[len(args)-1], nil
		}
	}
	args := strings.Fields(lang.RunCmd)
	if len(args) == 0 {
		return "", fmt.Errorf("empty run command for language %q", lang.ID)
	}
	return args[len(args)-1], nil
}

func cpuCores(lang db.Language) float64 {
	if !lang.CpuCores.Valid {
		return 1.0
	}
	f8, err := lang.CpuCores.Float64Value()
	if err != nil || !f8.Valid || f8.Float64 <= 0 {
		return 1.0
	}
	return f8.Float64
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

type testCaseResult struct {
	testCaseID pgtype.UUID
	verdict    db.Verdict
	stdout     string
	stderr     string
	duration   time.Duration
}

func evaluateResults(testCases []db.TestCase, results []sandbox.TestResult) ([]testCaseResult, db.Verdict, error) {
	if len(results) != len(testCases) {
		return nil, "", fmt.Errorf("result count mismatch: got %d, want %d", len(results), len(testCases))
	}

	tcResults := make([]testCaseResult, len(testCases))
	overall := db.VerdictAccepted
	for i, tc := range testCases {
		verdict := verdictForResult(results[i], tc.ExpectedOutput)
		tcResults[i] = testCaseResult{
			testCaseID: tc.ID,
			verdict:    verdict,
			stdout:     results[i].Stdout,
			stderr:     results[i].Stderr,
			duration:   results[i].Duration,
		}
		overall = combineVerdicts(overall, verdict)
	}
	return tcResults, overall, nil
}

func verdictForResult(res sandbox.TestResult, expectedOutput string) db.Verdict {
	if res.TimeLimitExceeded {
		return db.VerdictTimeLimitExceeded
	}
	if res.ExitCode == 137 {
		return db.VerdictMemoryLimitExceeded
	}
	if res.ExitCode != 0 {
		return db.VerdictRuntimeError
	}
	if strings.TrimSpace(res.Stdout) == strings.TrimSpace(expectedOutput) {
		return db.VerdictAccepted
	}
	return db.VerdictWrongAnswer
}

func combineVerdicts(current, next db.Verdict) db.Verdict {
	if current == db.VerdictAccepted {
		return next
	}
	return current
}

// durationToMillis converts a duration to milliseconds, clamped to the
// int32 range. Execution timeouts are bounded by language limits, so overflow
// is not expected in practice; the clamp satisfies gosec G115.
func durationToMillis(d time.Duration) int32 {
	ms := d.Milliseconds()
	if ms > math.MaxInt32 {
		return math.MaxInt32
	}
	if ms < math.MinInt32 {
		return math.MinInt32
	}
	return int32(ms)
}
