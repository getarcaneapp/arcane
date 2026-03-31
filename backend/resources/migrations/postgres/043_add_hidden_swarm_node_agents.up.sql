ALTER TABLE environments ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE environments ADD COLUMN parent_environment_id TEXT;
ALTER TABLE environments ADD COLUMN swarm_node_id TEXT;

CREATE INDEX IF NOT EXISTS idx_environments_hidden ON environments(hidden);
CREATE INDEX IF NOT EXISTS idx_environments_parent_environment_id ON environments(parent_environment_id);
CREATE INDEX IF NOT EXISTS idx_environments_swarm_node_id ON environments(swarm_node_id);
CREATE INDEX IF NOT EXISTS idx_environments_parent_swarm_node ON environments(parent_environment_id, swarm_node_id);
