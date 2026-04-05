CREATE TABLE IF NOT EXISTS swarm_stack_projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dir_name TEXT,
    environment_id TEXT NOT NULL,
    path TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_environment_id ON swarm_stack_projects(environment_id);
CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_name ON swarm_stack_projects(name);
CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_dir_name_not_null ON swarm_stack_projects(dir_name) WHERE dir_name IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_stack_projects_path_unique ON swarm_stack_projects(path);
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_stack_projects_environment_name_unique ON swarm_stack_projects(environment_id, name);
