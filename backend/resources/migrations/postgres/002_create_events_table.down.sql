DROP INDEX IF EXISTS idx_events_timestamp;
DROP INDEX IF EXISTS idx_events_environment_id;
DROP INDEX IF EXISTS idx_events_user_id;
DROP INDEX IF EXISTS idx_events_resource_id;
DROP INDEX IF EXISTS idx_events_resource_type;
DROP INDEX IF EXISTS idx_events_severity;
DROP INDEX IF EXISTS idx_events_type;

DROP TABLE IF EXISTS events;