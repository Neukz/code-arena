package worker_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/Neukz/code-arena/internal/sandbox"
	"github.com/Neukz/code-arena/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
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

func closeWithErrorCheck(t *testing.T, closer interface{ Close() error }) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func startPostgresForWorker(t *testing.T) (*postgres.PostgresContainer, string) {
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

func runMigrationsForWorker(t *testing.T, connStr string) {
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

func loadSeedForWorker(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	seedSQL, err := os.ReadFile(seedProblemsPath())
	if err != nil {
		t.Fatalf("failed to read seed file: %v", err)
	}
	if _, err := pool.Exec(ctx, string(seedSQL)); err != nil {
		t.Fatalf("failed to execute seed sql: %v", err)
	}
}

func setupDBForWorker(t *testing.T) (*db.Queries, *pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	ctr, connStr := startPostgresForWorker(t)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to create pool: %v", err)
	}

	if err := waitForPostgresForWorker(ctx, pool); err != nil {
		pool.Close()
		_ = ctr.Terminate(ctx)
		t.Fatalf("postgres did not become ready: %v", err)
	}

	runMigrationsForWorker(t, connStr)
	loadSeedForWorker(t, pool)

	cleanup := func() {
		pool.Close()
		_ = ctr.Terminate(ctx)
	}
	return db.New(pool), pool, cleanup
}

func waitForPostgresForWorker(ctx context.Context, pool *pgxpool.Pool) error {
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

func startRabbitMQForWorker(t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "rabbitmq:4.3-management-alpine",
		ExposedPorts: []string{"5672/tcp"},
		WaitingFor:   wait.ForListeningPort("5672/tcp"),
		Env: map[string]string{
			"RABBITMQ_DEFAULT_USER": "codearena",
			"RABBITMQ_DEFAULT_PASS": "codearena",
		},
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start rabbitmq container: %v", err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5672")
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to get mapped port: %v", err)
	}

	url := fmt.Sprintf("amqp://codearena:codearena@%s:%s/", host, port.Port())
	return ctr, url
}

func waitForQueueEmptyForWorker(t *testing.T, conn *amqp.Connection, name string) error {
	t.Helper()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer closeWithErrorCheck(t, ch)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for {
		q, err := ch.QueueDeclarePassive(name, true, false, false, false, nil)
		if err != nil {
			return err
		}
		if q.Messages == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("queue %q was not empty after 10s (had %d messages)", name, q.Messages)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// fakeRunner returns reversed input as output, matching the reverse-string problem,
// without touching Docker.
type fakeRunner struct{}

func (f *fakeRunner) Run(ctx context.Context, req sandbox.RunRequest) ([]sandbox.TestResult, error) {
	results := make([]sandbox.TestResult, len(req.TestCases))
	for i, tc := range req.TestCases {
		reversed := reverseString(tc.Input)
		results[i] = sandbox.TestResult{Stdout: reversed, ExitCode: 0, Duration: 10 * time.Millisecond}
	}
	return results, nil
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func TestWorker_EndToEnd(t *testing.T) {
	ctx := context.Background()

	queries, _, cleanupDB := setupDBForWorker(t)
	defer cleanupDB()

	rabbitCtr, amqpURL := startRabbitMQForWorker(t)
	defer func() { _ = rabbitCtr.Terminate(ctx) }()

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("failed to dial rabbitmq: %v", err)
	}
	defer closeWithErrorCheck(t, conn)

	pub, err := queue.NewPublisher(conn)
	if err != nil {
		t.Fatalf("failed to create publisher: %v", err)
	}
	defer closeWithErrorCheck(t, pub)

	problem, err := queries.GetProblemBySlug(ctx, "reverse-string")
	if err != nil {
		t.Fatalf("failed to get problem: %v", err)
	}
	testCases, err := queries.GetTestCasesByProblemID(ctx, problem.ID)
	if err != nil {
		t.Fatalf("failed to get test cases: %v", err)
	}
	if len(testCases) == 0 {
		t.Fatal("expected test cases for reverse-string")
	}

	sub, err := queries.CreateSubmission(ctx, db.CreateSubmissionParams{
		ProblemID:  problem.ID,
		LanguageID: "python",
		SourceCode: "print(input()[::-1])",
	})
	if err != nil {
		t.Fatalf("failed to create submission: %v", err)
	}

	job := models.SubmissionJob{
		SubmissionID: sub.ID.String(),
		ProblemID:    problem.ID.String(),
		LanguageID:   "python",
		SourceCode:   sub.SourceCode,
	}

	pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
	defer pcancel()
	if err := pub.Publish(pctx, job); err != nil {
		t.Fatalf("failed to publish job: %v", err)
	}

	processor := worker.NewProcessor(queries, &fakeRunner{}, discardLogger())
	handler := worker.Recover(processor.Process, queries, discardLogger())
	consumer, err := queue.NewConsumer(conn, handler, discardLogger(), 1, 3)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}
	defer closeWithErrorCheck(t, consumer)

	consumerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Start(consumerCtx)
	}()

	if err := waitForQueueEmptyForWorker(t, conn, queue.QueueName); err != nil {
		cancel()
		<-errCh
		t.Fatalf("main queue not empty: %v", err)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for consumer to stop")
	}

	completed, err := queries.GetSubmissionByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to get submission: %v", err)
	}
	if completed.Status != db.SubmissionStatusCompleted {
		t.Errorf("submission status = %v, want completed", completed.Status)
	}
	if !completed.OverallVerdict.Valid || completed.OverallVerdict.Verdict != db.VerdictAccepted {
		t.Errorf("overall verdict = %v, want accepted", completed.OverallVerdict)
	}

	results, err := queries.GetSubmissionResultsBySubmissionID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to get results: %v", err)
	}
	if len(results) != len(testCases) {
		t.Errorf("expected %d results, got %d", len(testCases), len(results))
	}
}
