package worker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/sandbox"
	"github.com/Neukz/code-arena/internal/worker"
	"github.com/jackc/pgx/v5/pgtype"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testUUID(n byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{n, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Valid: true}
}

type stubRepository struct {
	submission         db.Submission
	language           db.Language
	problem            db.Problem
	testCases          []db.TestCase
	err                error
	markRunningID      pgtype.UUID
	results            []db.UpsertSubmissionResultParams
	completed          *db.MarkSubmissionCompletedParams
	failed             *db.MarkSubmissionFailedParams
	getSubmissionCalls int
}

func (s *stubRepository) GetSubmissionByID(ctx context.Context, id pgtype.UUID) (db.Submission, error) {
	s.getSubmissionCalls++
	if s.err != nil {
		return db.Submission{}, s.err
	}
	return s.submission, nil
}

func (s *stubRepository) MarkSubmissionRunning(ctx context.Context, id pgtype.UUID) error {
	s.markRunningID = id
	return nil
}

func (s *stubRepository) GetLanguageByID(ctx context.Context, id string) (db.Language, error) {
	if s.err != nil {
		return db.Language{}, s.err
	}
	return s.language, nil
}

func (s *stubRepository) GetProblemByID(ctx context.Context, id pgtype.UUID) (db.Problem, error) {
	if s.err != nil {
		return db.Problem{}, s.err
	}
	return s.problem, nil
}

func (s *stubRepository) GetTestCasesByProblemID(ctx context.Context, problemID pgtype.UUID) ([]db.TestCase, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.testCases, nil
}

func (s *stubRepository) UpsertSubmissionResult(ctx context.Context, arg db.UpsertSubmissionResultParams) error {
	s.results = append(s.results, arg)
	return nil
}

func (s *stubRepository) MarkSubmissionCompleted(ctx context.Context, arg db.MarkSubmissionCompletedParams) error {
	s.completed = &arg
	return nil
}

func (s *stubRepository) MarkSubmissionFailed(ctx context.Context, arg db.MarkSubmissionFailedParams) error {
	s.failed = &arg
	return nil
}

type stubRunner struct {
	results []sandbox.TestResult
	err     error
}

func (s *stubRunner) Run(ctx context.Context, req sandbox.RunRequest) ([]sandbox.TestResult, error) {
	return s.results, s.err
}

func TestProcess_AcceptedAll(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "python",
			DockerImage:    "codearena/executor-python:latest",
			RunCmd:         "python3 /sandbox/main.py",
			MemoryLimitMb:  256,
			TimeoutSeconds: 10,
		},
		testCases: []db.TestCase{
			{ID: testUUID(1), Input: "hello", ExpectedOutput: "olleh"},
			{ID: testUUID(2), Input: "world", ExpectedOutput: "dlrow"},
		},
	}
	runner := &stubRunner{
		results: []sandbox.TestResult{
			{Stdout: "olleh", ExitCode: 0, Duration: 100 * time.Millisecond},
			{Stdout: "dlrow", ExitCode: 0, Duration: 150 * time.Millisecond},
		},
	}

	p := worker.NewProcessor(repo, runner, discardLogger())
	job := models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a12",
		LanguageID:   "python",
		SourceCode:   "print(input()[::-1])",
	}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	var wantID pgtype.UUID
	_ = wantID.Scan(job.SubmissionID)
	if repo.markRunningID != wantID {
		t.Errorf("MarkSubmissionRunning id = %v, want %v", repo.markRunningID, wantID)
	}
	if len(repo.results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(repo.results))
	}
	if repo.results[0].Verdict != db.VerdictAccepted {
		t.Errorf("result[0] verdict = %v, want accepted", repo.results[0].Verdict)
	}
	if repo.results[1].Verdict != db.VerdictAccepted {
		t.Errorf("result[1] verdict = %v, want accepted", repo.results[1].Verdict)
	}
	if repo.completed == nil {
		t.Fatal("expected submission to be marked completed")
	}
	if !repo.completed.OverallVerdict.Valid || repo.completed.OverallVerdict.Verdict != db.VerdictAccepted {
		t.Errorf("overall verdict = %v, want accepted", repo.completed.OverallVerdict)
	}
}

func TestProcess_WrongAnswer(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "python",
			DockerImage:    "codearena/executor-python:latest",
			RunCmd:         "python3 /sandbox/main.py",
			MemoryLimitMb:  256,
			TimeoutSeconds: 10,
		},
		testCases: []db.TestCase{
			{ID: testUUID(1), Input: "2 7 11 15\n9", ExpectedOutput: "0 1"},
		},
	}
	runner := &stubRunner{
		results: []sandbox.TestResult{{Stdout: "1 0", ExitCode: 0, Duration: 100 * time.Millisecond}},
	}

	p := worker.NewProcessor(repo, runner, discardLogger())
	job := models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a21",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a22",
		LanguageID:   "python",
		SourceCode:   "print('1 0')",
	}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if repo.results[0].Verdict != db.VerdictWrongAnswer {
		t.Errorf("verdict = %v, want wrong_answer", repo.results[0].Verdict)
	}
	if repo.completed == nil || !repo.completed.OverallVerdict.Valid || repo.completed.OverallVerdict.Verdict != db.VerdictWrongAnswer {
		t.Fatalf("expected overall verdict wrong_answer, got %v", repo.completed)
	}
}

func TestProcess_RuntimeError(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "python",
			DockerImage:    "codearena/executor-python:latest",
			RunCmd:         "python3 /sandbox/main.py",
			MemoryLimitMb:  256,
			TimeoutSeconds: 10,
		},
		testCases: []db.TestCase{{ID: testUUID(1), ExpectedOutput: "ok"}},
	}
	runner := &stubRunner{
		results: []sandbox.TestResult{{Stderr: "Traceback", ExitCode: 1, Duration: 50 * time.Millisecond}},
	}

	p := worker.NewProcessor(repo, runner, discardLogger())
	if err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a31",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a32",
		LanguageID:   "python",
	}); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if repo.results[0].Verdict != db.VerdictRuntimeError {
		t.Errorf("verdict = %v, want runtime_error", repo.results[0].Verdict)
	}
	if repo.completed.OverallVerdict.Verdict != db.VerdictRuntimeError {
		t.Errorf("overall verdict = %v, want runtime_error", repo.completed.OverallVerdict)
	}
}

func TestProcess_TimeLimitExceeded(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "python",
			DockerImage:    "codearena/executor-python:latest",
			RunCmd:         "python3 /sandbox/main.py",
			MemoryLimitMb:  256,
			TimeoutSeconds: 10,
		},
		testCases: []db.TestCase{{ID: testUUID(1), ExpectedOutput: "ok"}},
	}
	runner := &stubRunner{
		results: []sandbox.TestResult{{TimeLimitExceeded: true, Duration: 10 * time.Second}},
	}

	p := worker.NewProcessor(repo, runner, discardLogger())
	if err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a41",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a42",
		LanguageID:   "python",
	}); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if repo.results[0].Verdict != db.VerdictTimeLimitExceeded {
		t.Errorf("verdict = %v, want time_limit_exceeded", repo.results[0].Verdict)
	}
	if repo.completed.OverallVerdict.Verdict != db.VerdictTimeLimitExceeded {
		t.Errorf("overall verdict = %v, want time_limit_exceeded", repo.completed.OverallVerdict)
	}
}

func TestProcess_CompileError(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "go",
			DockerImage:    "codearena/executor-go:latest",
			CompileCmd:     pgtype.Text{String: "go build -o /sandbox/main /sandbox/main.go", Valid: true},
			RunCmd:         "/sandbox/main",
			MemoryLimitMb:  512,
			TimeoutSeconds: 30,
		},
		testCases: []db.TestCase{
			{ID: testUUID(1), ExpectedOutput: "ok"},
			{ID: testUUID(2), ExpectedOutput: "ok"},
		},
	}
	runner := &stubRunner{err: fmt.Errorf("%w: syntax error", sandbox.ErrCompileFailed)}

	p := worker.NewProcessor(repo, runner, discardLogger())
	if err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a51",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a52",
		LanguageID:   "go",
	}); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(repo.results) != 2 {
		t.Fatalf("expected 2 compile_error results, got %d", len(repo.results))
	}
	for _, r := range repo.results {
		if r.Verdict != db.VerdictCompileError {
			t.Errorf("verdict = %v, want compile_error", r.Verdict)
		}
	}
	if repo.completed.OverallVerdict.Verdict != db.VerdictCompileError {
		t.Errorf("overall verdict = %v, want compile_error", repo.completed.OverallVerdict)
	}
}

func TestProcess_AlreadyCompleted(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusCompleted},
	}
	runner := &stubRunner{err: errors.New("should not run")}

	p := worker.NewProcessor(repo, runner, discardLogger())
	if err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a61",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a62",
		LanguageID:   "python",
	}); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if repo.markRunningID.Valid {
		t.Error("expected MarkSubmissionRunning not to be called")
	}
	if len(repo.results) > 0 {
		t.Errorf("expected no results, got %d", len(repo.results))
	}
}

func TestProcess_SandboxError(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "python",
			DockerImage:    "codearena/executor-python:latest",
			RunCmd:         "python3 /sandbox/main.py",
			MemoryLimitMb:  256,
			TimeoutSeconds: 10,
		},
		testCases: []db.TestCase{{ID: testUUID(1), ExpectedOutput: "ok"}},
	}
	runner := &stubRunner{err: errors.New("docker unavailable")}

	p := worker.NewProcessor(repo, runner, discardLogger())
	err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a71",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a72",
		LanguageID:   "python",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if repo.failed == nil {
		t.Fatal("expected submission to be marked failed")
	}
}

func TestProcess_RespectsGoCompileCommand(t *testing.T) {
	repo := &stubRepository{
		submission: db.Submission{Status: db.SubmissionStatusPending},
		language: db.Language{
			ID:             "go",
			DockerImage:    "codearena/executor-go:latest",
			CompileCmd:     pgtype.Text{String: "go build -o /sandbox/main /sandbox/main.go", Valid: true},
			RunCmd:         "/sandbox/main",
			MemoryLimitMb:  512,
			TimeoutSeconds: 30,
		},
		testCases: []db.TestCase{{ID: testUUID(1), ExpectedOutput: "ok"}},
	}

	var captured sandbox.RunRequest
	runner := &funcRunner{
		runFunc: func(req sandbox.RunRequest) ([]sandbox.TestResult, error) {
			captured = req
			return []sandbox.TestResult{{Stdout: "ok", ExitCode: 0}}, nil
		},
	}

	p := worker.NewProcessor(repo, runner, discardLogger())
	if err := p.Process(context.Background(), models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a81",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a82",
		LanguageID:   "go",
		SourceCode:   "package main",
	}); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if captured.SandboxPath != "/sandbox/main.go" {
		t.Errorf("SandboxPath = %q, want /sandbox/main.go", captured.SandboxPath)
	}
	if len(captured.CompileCmd) == 0 {
		t.Error("expected CompileCmd to be set for Go")
	}
	if captured.Image != "codearena/executor-go:latest" {
		t.Errorf("Image = %q, want codearena/executor-go:latest", captured.Image)
	}
}

type funcRunner struct {
	runFunc func(req sandbox.RunRequest) ([]sandbox.TestResult, error)
}

func (f *funcRunner) Run(ctx context.Context, req sandbox.RunRequest) ([]sandbox.TestResult, error) {
	return f.runFunc(req)
}

func TestRecover_PanicsAreConvertedToErrors(t *testing.T) {
	repo := &stubRepository{}
	panicking := func(ctx context.Context, job models.SubmissionJob) error {
		panic("boom")
	}

	handler := worker.Recover(panicking, repo, discardLogger())
	err := handler(context.Background(), models.SubmissionJob{SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a91"})
	if err == nil {
		t.Fatal("expected error after panic recovery")
	}
	if err.Error() != "panic: boom" {
		t.Errorf("error = %q, want panic: boom", err.Error())
	}
	if repo.failed == nil {
		t.Fatal("expected submission to be marked failed after panic")
	}
}

func TestRecover_NoPanicPassesThrough(t *testing.T) {
	repo := &stubRepository{}
	handler := worker.Recover(func(ctx context.Context, job models.SubmissionJob) error {
		return nil
	}, repo, discardLogger())

	if err := handler(context.Background(), models.SubmissionJob{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.failed != nil {
		t.Error("expected MarkSubmissionFailed not to be called")
	}
}

func TestRecover_BadSubmissionIDStillReturnsError(t *testing.T) {
	repo := &stubRepository{}
	panicking := func(ctx context.Context, job models.SubmissionJob) error {
		panic("boom")
	}

	handler := worker.Recover(panicking, repo, discardLogger())
	err := handler(context.Background(), models.SubmissionJob{SubmissionID: "not-a-uuid"})
	if err == nil {
		t.Fatal("expected error after panic recovery")
	}
	if repo.failed != nil {
		t.Error("expected MarkSubmissionFailed not to be called for invalid submission id")
	}
}
