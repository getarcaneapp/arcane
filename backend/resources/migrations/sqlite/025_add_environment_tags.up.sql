-- Create environment_tags junction table for efficient tag-based filtering
CREATE TABLE IF NOT EXISTS environment_tags (
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    PRIMARY KEY (environment_id, tag)
);

-- Create index for efficient tag lookups
CREATE INDEX IF NOT EXISTS idx_environment_tags_tag ON environment_tags(tag);

-- Create saved environment filters table
CREATE TABLE IF NOT EXISTS environment_filters (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    is_default INTEGER DEFAULT 0,
    search_query TEXT DEFAULT '',
    selected_tags TEXT DEFAULT '[]',
    excluded_tags TEXT DEFAULT '[]',
    tag_mode TEXT DEFAULT 'any',
    status_filter TEXT DEFAULT 'all',
    group_by TEXT DEFAULT 'none',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- Create index for efficient user-based queries
CREATE INDEX IF NOT EXISTS idx_environment_filters_user_id ON environment_filters(user_id);

-- Ensure only one default filter per user
CREATE UNIQUE INDEX IF NOT EXISTS idx_environment_filters_user_default 
ON environment_filters(user_id) WHERE is_default = 1;