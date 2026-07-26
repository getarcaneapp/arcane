-- +goose Up
CREATE TABLE snippets (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    script TEXT NOT NULL,
    parameters TEXT NOT NULL DEFAULT '[]',
    working_dir TEXT,
    timeout_sec INTEGER NOT NULL DEFAULT 60,
    schedule TEXT,
    schedule_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule_parameters TEXT NOT NULL DEFAULT '{}',
    last_run_at TIMESTAMPTZ,
    last_run_status TEXT,
    created_by_user_id TEXT
);

CREATE UNIQUE INDEX idx_snippets_environment_id_name ON snippets(environment_id, name);
CREATE INDEX idx_snippets_environment_id ON snippets(environment_id);
CREATE INDEX idx_snippets_schedule_enabled ON snippets(schedule_enabled);

CREATE TABLE snippet_runs (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    snippet_id TEXT NOT NULL REFERENCES snippets(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL,
    trigger_source TEXT NOT NULL,
    status TEXT NOT NULL,
    exit_code INTEGER,
    parameters TEXT NOT NULL DEFAULT '{}',
    output TEXT,
    error TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    started_by_user_id TEXT,
    started_by_username TEXT
);

CREATE INDEX idx_snippet_runs_snippet_id_started_at ON snippet_runs(snippet_id, started_at);

-- +goose Down
DROP TABLE IF EXISTS snippet_runs;
DROP TABLE IF EXISTS snippets;
