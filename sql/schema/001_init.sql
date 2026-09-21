-- +goose Up
CREATE TYPE problem_difficulty AS ENUM ('easy', 'medium', 'hard');

CREATE TYPE submission_status AS ENUM ('pending', 'running', 'completed', 'failed');

CREATE TYPE verdict AS ENUM (
    'accepted',
    'wrong_answer',
    'runtime_error',
    'time_limit_exceeded',
    'memory_limit_exceeded',
    'compile_error',
    'internal_error'
);

CREATE TABLE languages (
    id              TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    docker_image    TEXT NOT NULL,
    compile_cmd     TEXT,
    run_cmd         TEXT NOT NULL,
    cpu_cores       NUMERIC NOT NULL DEFAULT 1.0,
    memory_limit_mb INT NOT NULL DEFAULT 256,
    timeout_seconds INT NOT NULL DEFAULT 10
);

CREATE TABLE problems (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL,
    difficulty  problem_difficulty NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE test_cases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    problem_id      UUID NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    input           TEXT NOT NULL,
    expected_output TEXT NOT NULL,
    is_hidden       BOOLEAN NOT NULL DEFAULT false,
    display_order   INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE submissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    problem_id      UUID NOT NULL REFERENCES problems(id),
    language_id     TEXT NOT NULL REFERENCES languages(id),
    source_code     TEXT NOT NULL,
    status          submission_status NOT NULL DEFAULT 'pending',
    overall_verdict verdict,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE TABLE submission_results (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id     UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    test_case_id      UUID NOT NULL REFERENCES test_cases(id),
    verdict           verdict NOT NULL,
    stdout            TEXT,
    stderr            TEXT,
    execution_time_ms INT,
    memory_used_mb    INT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (submission_id, test_case_id)
);

-- +goose Down
DROP TABLE IF EXISTS submission_results CASCADE;
DROP TABLE IF EXISTS submissions CASCADE;
DROP TABLE IF EXISTS test_cases CASCADE;
DROP TABLE IF EXISTS problems CASCADE;
DROP TABLE IF EXISTS languages CASCADE;
DROP TYPE IF EXISTS verdict CASCADE;
DROP TYPE IF EXISTS submission_status CASCADE;
DROP TYPE IF EXISTS problem_difficulty CASCADE;
