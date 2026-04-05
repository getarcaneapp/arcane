CREATE TABLE IF NOT EXISTS swarm_stack_projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    dir_name VARCHAR(255),
    environment_id VARCHAR(255) NOT NULL,
    path TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_environment_id ON swarm_stack_projects(environment_id);
CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_name ON swarm_stack_projects(name);
CREATE INDEX IF NOT EXISTS idx_swarm_stack_projects_dir_name_not_null ON swarm_stack_projects(dir_name) WHERE dir_name IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_stack_projects_path_unique ON swarm_stack_projects(path);
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_stack_projects_environment_name_unique ON swarm_stack_projects(environment_id, name);
