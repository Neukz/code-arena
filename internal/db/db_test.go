package db_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var projectRoot string

func init() {
	_, b, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(b), "..", "..")

	if os.Getenv("DOCKER_HOST") == "" {
		for _, sock := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
			if _, err := os.Stat(sock); err == nil {
				_ = os.Setenv("DOCKER_HOST", "unix://"+sock)
				break
			}
		}
	}
}

func startPostgres(t *testing.T) (*postgres.PostgresContainer, string) {
	t.Helper()
	ctx := context.Background()

	ctr, err := postgres.Run(
		ctx,
		"postgres:18.6-alpine",
		postgres.WithDatabase("codearena_test"),
		postgres.WithUsername("codearena_test"),
		postgres.WithPassword("codearena_test"),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}
	return ctr, connStr
}

func migrationDir() string {
	dir := os.Getenv("GOOSE_MIGRATION_DIR")
	if dir == "" {
		dir = "sql/schema"
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(projectRoot, dir)
}

func seedProblemsPath() string {
	p := os.Getenv("SEED_PROBLEMS_PATH")
	if p == "" {
		p = "scripts/seed_problems.sql"
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(projectRoot, p)
}

func runMigrations(t *testing.T, connStr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var out []byte
	var err error
	for range 10 {
		// #nosec G204 - test-only invocation against an ephemeral testcontainer DB.
		cmd := exec.CommandContext(ctx, "goose", "-dir", migrationDir(), "postgres", connStr, "up")
		out, err = cmd.CombinedOutput()
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("failed to run migrations after retries: %v\n%s", err, out)
}

func loadSeed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	seedSQL, err := os.ReadFile(seedProblemsPath())
	if err != nil {
		t.Fatalf("failed to read seed file: %v", err)
	}

	_, err = pool.Exec(ctx, string(seedSQL))
	if err != nil {
		t.Fatalf("failed to execute seed sql: %v", err)
	}
}

func setup(t *testing.T) (*db.Queries, func()) {
	t.Helper()
	ctx := context.Background()

	ctr, connStr := startPostgres(t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to create pool: %v", err)
	}

	if err := waitForPostgres(ctx, pool); err != nil {
		pool.Close()
		_ = ctr.Terminate(ctx)
		t.Fatalf("postgres did not become ready: %v", err)
	}

	runMigrations(t, connStr)
	loadSeed(t, pool)

	cleanup := func() {
		pool.Close()
		_ = ctr.Terminate(ctx)
	}
	return db.New(pool), cleanup
}

func waitForPostgres(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		if err := pool.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func TestLanguages(t *testing.T) {
	queries, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	langs, err := queries.ListLanguages(ctx)
	if err != nil {
		t.Fatalf("ListLanguages: %v", err)
	}
	if len(langs) != 3 {
		t.Fatalf("expected 3 seeded languages, got %d", len(langs))
	}

	py, err := queries.GetLanguageByID(ctx, "python")
	if err != nil {
		t.Fatalf("GetLanguageByID: %v", err)
	}
	if py.DisplayName != "Python 3" {
		t.Errorf("unexpected python display name: %s", py.DisplayName)
	}
	if !py.CpuCores.Valid {
		t.Errorf("expected python cpu_cores to be set")
	}
}

func TestProblems(t *testing.T) {
	queries, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	// Seed data contains two problems.
	problems, err := queries.ListProblems(ctx, db.NullProblemDifficulty{})
	if err != nil {
		t.Fatalf("ListProblems: %v", err)
	}
	if len(problems) != 2 {
		t.Fatalf("expected 2 seeded problems, got %d", len(problems))
	}

	twoSum, err := queries.GetProblemBySlug(ctx, "two-sum")
	if err != nil {
		t.Fatalf("GetProblemBySlug: %v", err)
	}
	if twoSum.Title != "Two Sum" {
		t.Errorf("unexpected problem title: %s", twoSum.Title)
	}
	if twoSum.Difficulty != db.ProblemDifficultyEasy {
		t.Errorf("unexpected difficulty: %v", twoSum.Difficulty)
	}

	filtered, err := queries.ListProblems(ctx, db.NullProblemDifficulty{
		ProblemDifficulty: db.ProblemDifficultyEasy,
		Valid:             true,
	})
	if err != nil {
		t.Fatalf("ListProblems with filter: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 easy problems, got %d", len(filtered))
	}
}

func TestTestCases(t *testing.T) {
	queries, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	problem, err := queries.GetProblemBySlug(ctx, "two-sum")
	if err != nil {
		t.Fatalf("GetProblemBySlug: %v", err)
	}

	sampleCases, err := queries.GetSampleTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		t.Fatalf("GetSampleTestCasesByProblemID: %v", err)
	}
	if len(sampleCases) != 2 {
		t.Errorf("expected 2 sample cases for two-sum, got %d", len(sampleCases))
	}

	allCases, err := queries.GetTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		t.Fatalf("GetTestCasesByProblemID: %v", err)
	}
	if len(allCases) != 3 {
		t.Errorf("expected 3 total cases for two-sum, got %d", len(allCases))
	}

}

func TestSubmissions(t *testing.T) {
	queries, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	problem, err := queries.GetProblemBySlug(ctx, "reverse-string")
	if err != nil {
		t.Fatalf("GetProblemBySlug: %v", err)
	}

	sub, err := queries.CreateSubmission(ctx, db.CreateSubmissionParams{
		ProblemID:  problem.ID,
		LanguageID: "python",
		SourceCode: "print(input()[::-1])",
	})
	if err != nil {
		t.Fatalf("CreateSubmission: %v", err)
	}
	if sub.Status != db.SubmissionStatusPending {
		t.Errorf("expected status pending, got %v", sub.Status)
	}

	if err := queries.MarkSubmissionRunning(ctx, sub.ID); err != nil {
		t.Fatalf("MarkSubmissionRunning: %v", err)
	}

	fetched, err := queries.GetSubmissionByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubmissionByID: %v", err)
	}
	if fetched.Status != db.SubmissionStatusRunning {
		t.Errorf("expected status running, got %v", fetched.Status)
	}

	if err := queries.MarkSubmissionCompleted(ctx, db.MarkSubmissionCompletedParams{
		ID:             sub.ID,
		OverallVerdict: db.NullVerdict{Verdict: db.VerdictAccepted, Valid: true},
	}); err != nil {
		t.Fatalf("MarkSubmissionCompleted: %v", err)
	}

	completed, err := queries.GetSubmissionByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubmissionByID after complete: %v", err)
	}
	if completed.Status != db.SubmissionStatusCompleted {
		t.Errorf("expected status completed, got %v", completed.Status)
	}
	if !completed.OverallVerdict.Valid || completed.OverallVerdict.Verdict != db.VerdictAccepted {
		t.Errorf("expected overall verdict accepted, got %v", completed.OverallVerdict)
	}
	if !completed.CompletedAt.Valid {
		t.Errorf("expected completed_at to be set")
	}
}

func TestSubmissionResults(t *testing.T) {
	queries, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	problem, err := queries.GetProblemBySlug(ctx, "two-sum")
	if err != nil {
		t.Fatalf("GetProblemBySlug: %v", err)
	}

	sub, err := queries.CreateSubmission(ctx, db.CreateSubmissionParams{
		ProblemID:  problem.ID,
		LanguageID: "go",
		SourceCode: "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"0 1\") }",
	})
	if err != nil {
		t.Fatalf("CreateSubmission: %v", err)
	}

	sampleCases, err := queries.GetSampleTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		t.Fatalf("GetSampleTestCasesByProblemID: %v", err)
	}
	if len(sampleCases) == 0 {
		t.Fatalf("expected sample test cases")
	}

	hiddenCases, err := queries.GetTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		t.Fatalf("GetTestCasesByProblemID: %v", err)
	}

	var hiddenID pgtype.UUID
	var foundHidden bool
	for _, tc := range hiddenCases {
		if tc.IsHidden {
			hiddenID = tc.ID
			foundHidden = true
		}
		if err := queries.UpsertSubmissionResult(ctx, db.UpsertSubmissionResultParams{
			SubmissionID: sub.ID,
			TestCaseID:   tc.ID,
			Verdict:      db.VerdictAccepted,
			Stdout:       pgtype.Text{String: "0 1", Valid: true},
			Stderr:       pgtype.Text{String: "", Valid: true},
		}); err != nil {
			t.Fatalf("UpsertSubmissionResult: %v", err)
		}
	}
	if !foundHidden {
		t.Fatalf("expected at least one hidden test case")
	}

	results, err := queries.GetSubmissionResultsBySubmissionID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubmissionResultsBySubmissionID: %v", err)
	}
	if len(results) != len(hiddenCases) {
		t.Errorf("expected %d results, got %d", len(hiddenCases), len(results))
	}

	for _, r := range results {
		if r.TestCaseID == hiddenID {
			if !r.IsHidden {
				t.Errorf("expected hidden case to be marked hidden")
			}
		}
	}
}
