-- +goose Up
CREATE TABLE environment_update_jobs (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    status TEXT NOT NULL,
    user_id TEXT,
    username TEXT,
    manager_version_at_start TEXT,
    manager_digest_at_start TEXT,
    manager_target_version TEXT,
    results TEXT,
    error TEXT,
    completed_at DATETIME
);

CREATE INDEX idx_environment_update_jobs_status ON environment_update_jobs(status);

-- +goose Down
DROP TABLE IF EXISTS environment_update_jobs;
