CREATE TABLE IF NOT EXISTS images_table (
    id TEXT PRIMARY KEY,
    repo_tags TEXT,
    repo_digests TEXT,
    size BIGINT NOT NULL,
    virtual_size BIGINT NOT NULL DEFAULT 0,
    labels TEXT,
    created TIMESTAMP NOT NULL,
    repo TEXT,
    tag TEXT,
    in_use BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);