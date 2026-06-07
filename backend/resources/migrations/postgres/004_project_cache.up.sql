CREATE TABLE IF NOT EXISTS project_cache (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    service_count INTEGER DEFAULT 0,
    running_count INTEGER DEFAULT 0,
    auto_update BOOLEAN DEFAULT FALSE,
    is_external BOOLEAN DEFAULT FALSE,
    is_legacy BOOLEAN DEFAULT FALSE,
    is_remote BOOLEAN DEFAULT FALSE,
    last_modified TIMESTAMP NOT NULL,
    compose_hash VARCHAR(64) NOT NULL,
    cached_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_project_cache_status ON project_cache(status);
CREATE INDEX idx_project_cache_cached_at ON project_cache(cached_at);
CREATE INDEX idx_project_cache_auto_update ON project_cache(auto_update);