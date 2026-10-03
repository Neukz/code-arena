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
	pingErr                  error
	problems                 []db.Problem
	listProblemsErr          error
	problemBySlug            db.Problem
	getProblemBySlugErr      error
	problemByID              db.Problem
	getProblemByIDErr        error
	sampleTestCases          []db.TestCase
	getSampleTestCasesErr    error
	languages                []db.Language
	listLanguagesErr         error
	languageByID             db.Language
	getLanguageByIDErr       error
	createdSubmission        db.Submission
	createSubmissionErr      error
	markSubmissionFailedErr  error
	lastMarkSubmissionFailed db.MarkSubmissionFailedParams
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
