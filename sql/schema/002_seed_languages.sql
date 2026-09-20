-- +goose Up
INSERT INTO languages
    (id, display_name, docker_image, compile_cmd, run_cmd, cpu_cores, memory_limit_mb, timeout_seconds)
VALUES
    ('python', 'Python 3', 'codearena/executor-python:latest', NULL, 'python3 /sandbox/main.py', 1.0, 256, 10),
    ('javascript', 'JavaScript', 'codearena/executor-javascript:latest', NULL, 'node /sandbox/main.js', 1.0, 256, 10),
    ('go', 'Go', 'codearena/executor-go:latest', 'go build -o /sandbox/main /sandbox/main.go', '/sandbox/main', 1.0, 512, 30);

-- +goose Down
DELETE FROM languages WHERE id IN ('python', 'javascript', 'go');
