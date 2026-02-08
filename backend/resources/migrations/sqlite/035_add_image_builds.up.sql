-- Add image_builds table for storing build history
CREATE TABLE IF NOT EXISTS image_builds (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    user_id TEXT,
    username TEXT,
    status TEXT NOT NULL,
    provider TEXT,
    context_dir TEXT NOT NULL,
    dockerfile TEXT,
    target TEXT,
    tags TEXT,
    platforms TEXT,
    build_args TEXT,
    push BOOLEAN NOT NULL DEFAULT 0,
    load BOOLEAN NOT NULL DEFAULT 0,
    digest TEXT,
    error_message TEXT,
    output TEXT,
    output_truncated BOOLEAN NOT NULL DEFAULT 0,
    completed_at DATETIME,
    duration_ms INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- Create indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_image_builds_env ON image_builds(environment_id);
CREATE INDEX IF NOT EXISTS idx_image_builds_status ON image_builds(status);
CREATE INDEX IF NOT EXISTS idx_image_builds_created_at ON image_builds(created_at);
