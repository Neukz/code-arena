package api

import (
	"context"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// Querier describes the data access operations used by the API gateway.
// It matches the subset of *db.Queries that handlers need.
type Querier interface {
	Ping(ctx context.Context) (int32, error)
	ListProblems(ctx context.Context, difficulty db.NullProblemDifficulty) ([]db.Problem, error)
	GetProblemBySlug(ctx context.Context, slug string) (db.Problem, error)
	GetSampleTestCasesByProblemID(ctx context.Context, problemID pgtype.UUID) ([]db.TestCase, error)
	ListLanguages(ctx context.Context) ([]db.Language, error)
}
