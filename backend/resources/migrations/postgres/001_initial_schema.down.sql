-- Drop all tables in reverse order of dependencies (PostgreSQL)

DROP INDEX IF EXISTS idx_auto_update_start_time;
DROP INDEX IF EXISTS idx_auto_update_resource_id;
DROP INDEX IF EXISTS idx_image_update_tag;
DROP INDEX IF EXISTS idx_image_update_repository;
DROP INDEX IF EXISTS idx_images_tag;
DROP INDEX IF EXISTS idx_images_repo;
DROP INDEX IF EXISTS idx_containers_stack_id;
DROP INDEX IF EXISTS idx_user_sessions_token;
DROP INDEX IF EXISTS idx_user_sessions_user_id;
DROP INDEX IF EXISTS idx_settings_public;
DROP INDEX IF EXISTS idx_settings_key;
DROP INDEX IF EXISTS idx_events_timestamp;

DROP TABLE IF EXISTS auto_update_records;
DROP TABLE IF EXISTS container_registries;
DROP TABLE IF EXISTS compose_templates;
DROP TABLE IF EXISTS template_registries;
DROP TABLE IF EXISTS image_update_table;
DROP TABLE IF EXISTS networks_table;
DROP TABLE IF EXISTS volumes_table;
DROP TABLE IF EXISTS images_table;
DROP TABLE IF EXISTS containers_table;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS stacks_table;
DROP TABLE IF EXISTS user_sessions_table;
DROP TABLE IF EXISTS users_table;
DROP TABLE IF EXISTS settings;