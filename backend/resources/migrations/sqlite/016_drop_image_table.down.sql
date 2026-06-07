CREATE TABLE IF NOT EXISTS images_table (
    id TEXT PRIMARY KEY,
    repo_tags TEXT,
    repo_digests TEXT,
    size INTEGER NOT NULL,
    virtual_size INTEGER NOT NULL DEFAULT 0,
    labels TEXT,
    created DATETIME NOT NULL,
    repo TEXT,
    tag TEXT,
    in_use BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);