package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubStore struct {
	pingErr                         error
	problems                        []db.Problem
	listProblemsErr                 error
	problemBySlug                   db.Problem
	getProblemBySlugErr             error
	problemByID                     db.Problem
	getProblemByIDErr               error
	sampleTestCases                 []db.TestCase
	getSampleTestCasesErr           error
	languages                       []db.Language
	listLanguagesErr                error
	languageByID                    db.Language
	getLanguageByIDErr              error
	createdSubmission               db.Submission
	createSubmissionErr             error
	markSubmissionFailedErr         error
	lastMarkSubmissionFailed        db.MarkSubmissionFailedParams
	submissionByID                  db.Submission
	getSubmissionByIDErr            error
	submissionResultsBySubmissionID []db.GetSubmissionResultsBySubmissionIDRow
	getSubmissionResultsErr         error
}

func (s *stubStore) Ping(ctx context.Context) (int32, error) {
	return 1, s.pingErr
}

func (s *stubStore) ListProblems(ctx context.Context, difficulty db.NullProblemDifficulty) ([]db.Problem, error) {
	return s.problems, s.listProblemsErr
}

func (s *stubStore) GetProblemBySlug(ctx context.Context, slug string) (db.Problem, error) {
	return s.problemBySlug, s.getProblemBySlugErr
}

func (s *stubStore) GetProblemByID(ctx context.Context, id pgtype.UUID) (db.Problem, error) {
	return s.problemByID, s.getProblemByIDErr
}

func (s *stubStore) GetSampleTestCasesByProblemID(ctx context.Context, problemID pgtype.UUID) ([]db.TestCase, error) {
	return s.sampleTestCases, s.getSampleTestCasesErr
}

func (s *stubStore) ListLanguages(ctx context.Context) ([]db.Language, error) {
	return s.languages, s.listLanguagesErr
}

func (s *stubStore) GetLanguageByID(ctx context.Context, id string) (db.Language, error) {
	return s.languageByID, s.getLanguageByIDErr
}

func (s *stubStore) CreateSubmission(ctx context.Context, arg db.CreateSubmissionParams) (db.Submission, error) {
	if s.createSubmissionErr != nil {
		return db.Submission{}, s.createSubmissionErr
	}
	return s.createdSubmission, nil
}

func (s *stubStore) MarkSubmissionFailed(ctx context.Context, arg db.MarkSubmissionFailedParams) error {
	s.lastMarkSubmissionFailed = arg
	return s.markSubmissionFailedErr
}

func (s *stubStore) GetSubmissionByID(ctx context.Context, id pgtype.UUID) (db.Submission, error) {
	return s.submissionByID, s.getSubmissionByIDErr
}

func (s *stubStore) GetSubmissionResultsBySubmissionID(ctx context.Context, submissionID pgtype.UUID) ([]db.GetSubmissionResultsBySubmissionIDRow, error) {
	return s.submissionResultsBySubmissionID, s.getSubmissionResultsErr
}

func uuidBytes() [16]byte {
	return [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
}

func mustUUID(b [16]byte) pgtype.UUID {
	u := pgtype.UUID{Bytes: b, Valid: true}
	return u
}

func closeBody(t *testing.T, body io.ReadCloser) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}
}

func makeHandler(store *stubStore, publisher queue.JobPublisher) *Handler {
	return NewHandler(store, publisher, slog.Default())
}

func TestHealthz(t *testing.T) {
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestReadyz_OK(t *testing.T) {
	h := makeHandler(&stubStore{pingErr: nil}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ready"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestReadyz_Unavailable(t *testing.T) {
	h := makeHandler(&stubStore{pingErr: errors.New("db unreachable")}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"unavailable"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestReadyz_SetsRequestID(t *testing.T) {
	h := makeHandler(&stubStore{pingErr: nil}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}
}

func TestListProblems(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problems: []db.Problem{
			{
				ID:         uid,
				Slug:       "two-sum",
				Title:      "Two Sum",
				Difficulty: db.ProblemDifficultyEasy,
				CreatedAt:  pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/problems")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body ListProblemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(body.Data))
	}
	if body.Data[0].Slug != "two-sum" {
		t.Fatalf("expected slug two-sum, got %s", body.Data[0].Slug)
	}
	if body.Data[0].Difficulty != "easy" {
		t.Fatalf("expected difficulty easy, got %s", body.Data[0].Difficulty)
	}
}

func TestListProblems_DBError(t *testing.T) {
	store := &stubStore{listProblemsErr: errors.New("boom")}
	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/problems")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestGetProblem(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemBySlug: db.Problem{
			ID:          uid,
			Slug:        "two-sum",
			Title:       "Two Sum",
			Description: "Find two numbers.",
			Difficulty:  db.ProblemDifficultyEasy,
			CreatedAt:   pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			UpdatedAt:   pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true},
		},
		sampleTestCases: []db.TestCase{
			{
				ID:             uid,
				ProblemID:      uid,
				Name:           "Sample 1",
				Input:          "2 7 11 15\n9",
				ExpectedOutput: "0 1",
				IsHidden:       false,
			},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/problems/two-sum")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body ProblemDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Slug != "two-sum" {
		t.Fatalf("expected slug two-sum, got %s", body.Slug)
	}
	if len(body.TestCases) != 1 {
		t.Fatalf("expected 1 test case, got %d", len(body.TestCases))
	}
	if body.TestCases[0].Input != "2 7 11 15\n9" {
		t.Fatalf("unexpected input: %s", body.TestCases[0].Input)
	}
}

func TestGetProblem_NotFound(t *testing.T) {
	store := &stubStore{getProblemBySlugErr: pgx.ErrNoRows}
	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/problems/missing")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestListLanguages(t *testing.T) {
	store := &stubStore{
		languages: []db.Language{
			{ID: "python", DisplayName: "Python 3", MemoryLimitMb: 256, TimeoutSeconds: 10},
			{ID: "go", DisplayName: "Go", MemoryLimitMb: 512, TimeoutSeconds: 30},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/languages")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body ListLanguagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(body.Data))
	}
	if body.Data[0].ID != "python" {
		t.Fatalf("expected first language python, got %s", body.Data[0].ID)
	}
}

func TestListLanguages_DBError(t *testing.T) {
	store := &stubStore{listLanguagesErr: errors.New("boom")}
	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/languages")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemByID: db.Problem{
			ID: uid,
		},
		languageByID: db.Language{
			ID: "python",
		},
		createdSubmission: db.Submission{
			ID:         uid,
			ProblemID:  uid,
			LanguageID: "python",
			SourceCode: "print('hello')",
			Status:     db.SubmissionStatusPending,
		},
	}
	publisher := &queue.StubPublisher{}

	h := makeHandler(store, publisher)
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	var body CreateSubmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.SubmissionID != uid.String() {
		t.Fatalf("expected submission id %s, got %s", uid.String(), body.SubmissionID)
	}
}

func TestCreateSubmission_InvalidProblemID(t *testing.T) {
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  "not-a-uuid",
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_MissingProblemID(t *testing.T) {
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_MissingLanguageID(t *testing.T) {
	uid := mustUUID(uuidBytes())
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_MissingSourceCode(t *testing.T) {
	uid := mustUUID(uuidBytes())
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_CreateSubmissionDBError(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemByID: db.Problem{
			ID: uid,
		},
		languageByID: db.Language{
			ID: "python",
		},
		createSubmissionErr: errors.New("db write failed"),
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_ProblemNotFound(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		getProblemByIDErr: pgx.ErrNoRows,
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_LanguageNotFound(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemByID: db.Problem{
			ID: uid,
		},
		getLanguageByIDErr: pgx.ErrNoRows,
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_SourceCodeTooLong(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemByID: db.Problem{
			ID: uid,
		},
		languageByID: db.Language{
			ID: "python",
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: strings.Repeat("a", maxSourceCodeLength+1),
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateSubmission_PublishFailure(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		problemByID: db.Problem{
			ID: uid,
		},
		languageByID: db.Language{
			ID: "python",
		},
		createdSubmission: db.Submission{
			ID:         uid,
			ProblemID:  uid,
			LanguageID: "python",
			SourceCode: "print('hello')",
			Status:     db.SubmissionStatusPending,
		},
	}
	publisher := &queue.StubPublisher{Err: errors.New("queue unreachable")}

	h := makeHandler(store, publisher)
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(CreateSubmissionRequest{
		ProblemID:  uid.String(),
		LanguageID: "python",
		SourceCode: "print('hello')",
	})

	resp, err := http.Post(ts.URL+"/api/v1/submissions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", resp.StatusCode)
	}
	if !store.lastMarkSubmissionFailed.ErrorMessage.Valid {
		t.Fatal("expected submission to be marked failed")
	}
}

func TestGetSubmission_Pending(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{
		submissionByID: db.Submission{
			ID:         uid,
			ProblemID:  uid,
			LanguageID: "python",
			Status:     db.SubmissionStatusPending,
			CreatedAt:  pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			UpdatedAt:  pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/" + uid.String())
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body SubmissionStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ID != uid.String() {
		t.Fatalf("expected id %s, got %s", uid.String(), body.ID)
	}
	if body.Status != "pending" {
		t.Fatalf("expected status pending, got %s", body.Status)
	}
	if len(body.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(body.Results))
	}
	if body.CompletedAt != nil {
		t.Fatalf("expected nil completed_at, got %v", body.CompletedAt)
	}
}

func TestGetSubmission_Completed(t *testing.T) {
	uid := mustUUID(uuidBytes())
	resultID := mustUUID(uuidBytes())
	completedAt := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	store := &stubStore{
		submissionByID: db.Submission{
			ID:             uid,
			ProblemID:      uid,
			LanguageID:     "python",
			Status:         db.SubmissionStatusCompleted,
			OverallVerdict: db.NullVerdict{Verdict: db.VerdictAccepted, Valid: true},
			CreatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			UpdatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true},
			CompletedAt:    pgtype.Timestamptz{Time: completedAt, Valid: true},
		},
		submissionResultsBySubmissionID: []db.GetSubmissionResultsBySubmissionIDRow{
			{
				ID:              resultID,
				SubmissionID:    uid,
				TestCaseID:      uid,
				Verdict:         db.VerdictAccepted,
				Stdout:          pgtype.Text{String: "0 1", Valid: true},
				Stderr:          pgtype.Text{String: "", Valid: true},
				ExecutionTimeMs: pgtype.Int4{Int32: 42, Valid: true},
				MemoryUsedMb:    pgtype.Int4{Int32: 16, Valid: true},
				Name:            "Sample 1",
				IsHidden:        false,
			},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/" + uid.String())
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body SubmissionStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "completed" {
		t.Fatalf("expected status completed, got %s", body.Status)
	}
	if body.OverallVerdict != "accepted" {
		t.Fatalf("expected overall_verdict accepted, got %s", body.OverallVerdict)
	}
	if body.CompletedAt == nil || !body.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed_at %v, got %v", completedAt, body.CompletedAt)
	}
	if len(body.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(body.Results))
	}
	if body.Results[0].Verdict != "accepted" {
		t.Fatalf("expected verdict accepted, got %s", body.Results[0].Verdict)
	}
	if body.Results[0].Stdout != "0 1" {
		t.Fatalf("unexpected stdout: %s", body.Results[0].Stdout)
	}
	if body.Results[0].ExecutionTimeMs == nil || *body.Results[0].ExecutionTimeMs != 42 {
		t.Fatalf("expected execution_time_ms 42, got %v", body.Results[0].ExecutionTimeMs)
	}
	if body.Results[0].MemoryUsedMb == nil || *body.Results[0].MemoryUsedMb != 16 {
		t.Fatalf("expected memory_used_mb 16, got %v", body.Results[0].MemoryUsedMb)
	}
}

func TestGetSubmission_HidesHiddenCaseOutput(t *testing.T) {
	uid := mustUUID(uuidBytes())
	resultID := mustUUID(uuidBytes())
	store := &stubStore{
		submissionByID: db.Submission{
			ID:             uid,
			ProblemID:      uid,
			LanguageID:     "python",
			Status:         db.SubmissionStatusCompleted,
			OverallVerdict: db.NullVerdict{Verdict: db.VerdictAccepted, Valid: true},
			CreatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			UpdatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true},
		},
		submissionResultsBySubmissionID: []db.GetSubmissionResultsBySubmissionIDRow{
			{
				ID:              resultID,
				SubmissionID:    uid,
				TestCaseID:      uid,
				Verdict:         db.VerdictAccepted,
				Stdout:          pgtype.Text{String: "secret", Valid: true},
				Stderr:          pgtype.Text{String: "secret err", Valid: true},
				ExecutionTimeMs: pgtype.Int4{Int32: 10, Valid: true},
				Name:            "Hidden 1",
				IsHidden:        true,
			},
		},
	}

	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/" + uid.String())
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body SubmissionStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(body.Results))
	}
	if body.Results[0].Stdout != "" {
		t.Fatalf("expected hidden stdout to be empty, got %s", body.Results[0].Stdout)
	}
	if body.Results[0].Stderr != "" {
		t.Fatalf("expected hidden stderr to be empty, got %s", body.Results[0].Stderr)
	}
}

func TestGetSubmission_NotFound(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{getSubmissionByIDErr: pgx.ErrNoRows}
	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/" + uid.String())
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestGetSubmission_InvalidID(t *testing.T) {
	h := makeHandler(&stubStore{}, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/not-a-uuid")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestGetSubmission_DBError(t *testing.T) {
	uid := mustUUID(uuidBytes())
	store := &stubStore{getSubmissionByIDErr: errors.New("boom")}
	h := makeHandler(store, &queue.StubPublisher{})
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/submissions/" + uid.String())
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}
