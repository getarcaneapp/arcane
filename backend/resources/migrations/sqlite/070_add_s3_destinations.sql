-- +goose Up
CREATE TABLE s3_destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT,
    bucket TEXT NOT NULL,
    region TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    prefix TEXT,
    use_ssl INTEGER NOT NULL DEFAULT 1,
    force_path_style INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- +goose Down
DROP TABLE s3_destinations;
