DROP INDEX IF EXISTS idx_environments_parent_swarm_node;
DROP INDEX IF EXISTS idx_environments_swarm_node_id;
DROP INDEX IF EXISTS idx_environments_parent_environment_id;
DROP INDEX IF EXISTS idx_environments_hidden;

ALTER TABLE environments DROP COLUMN swarm_node_id;
ALTER TABLE environments DROP COLUMN parent_environment_id;
ALTER TABLE environments DROP COLUMN hidden;
