CREATE TABLE IF NOT EXISTS project_cache (
    id TEXT PRIMARY KEY,
    stack_id TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    service_count INTEGER DEFAULT 0,
    running_count INTEGER DEFAULT 0,
    auto_update BOOLEAN DEFAULT FALSE,
    is_external BOOLEAN DEFAULT FALSE,
    is_legacy BOOLEAN DEFAULT FALSE,
    is_remote BOOLEAN DEFAULT FALSE,
    last_modified DATETIME NOT NULL,
    compose_hash TEXT NOT NULL,
    cached_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX idx_project_cache_status ON project_cache(status);
CREATE INDEX idx_project_cache_cached_at ON project_cache(cached_at);
CREATE INDEX idx_project_cache_auto_update ON project_cache(auto_update);