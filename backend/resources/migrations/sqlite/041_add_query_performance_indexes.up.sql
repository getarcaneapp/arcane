CREATE INDEX IF NOT EXISTS idx_environments_access_token_not_null
ON environments(access_token)
WHERE access_token IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_environments_enabled_true
ON environments(id, name)
WHERE enabled = 1;

CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at_not_null
ON api_keys(expires_at)
WHERE expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_api_keys_user_managed_by_created_at
ON api_keys(user_id, managed_by, created_at, id);

CREATE INDEX IF NOT EXISTS idx_git_repositories_enabled_url
ON git_repositories(enabled, url);

CREATE INDEX IF NOT EXISTS idx_git_repositories_auth_type
ON git_repositories(auth_type);

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_auto_sync
ON gitops_syncs(environment_id, auto_sync);

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_auto_sync_true
ON gitops_syncs(id, environment_id, sync_interval, last_sync_at)
WHERE auto_sync = 1;

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_last_sync_status
ON gitops_syncs(environment_id, last_sync_status);

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_repository_id
ON gitops_syncs(environment_id, repository_id);

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_project_id
ON gitops_syncs(environment_id, project_id);

CREATE INDEX IF NOT EXISTS idx_projects_path
ON projects(path);

CREATE INDEX IF NOT EXISTS idx_projects_dir_name_not_null
ON projects(dir_name)
WHERE dir_name IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_compose_templates_lookup_name
ON compose_templates(is_remote, registry_id, name);

CREATE INDEX IF NOT EXISTS idx_compose_templates_lookup_description
ON compose_templates(is_remote, registry_id, description);

CREATE INDEX IF NOT EXISTS idx_volume_backups_volume_name_created_at
ON volume_backups(volume_name, created_at);

CREATE INDEX IF NOT EXISTS idx_image_builds_environment_created_at
ON image_builds(environment_id, created_at);

CREATE INDEX IF NOT EXISTS idx_image_builds_environment_status
ON image_builds(environment_id, status);

CREATE INDEX IF NOT EXISTS idx_events_environment_timestamp
ON events(environment_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_image_updates_repository_tag
ON image_updates(repository, tag);

CREATE INDEX IF NOT EXISTS idx_vulnerability_scans_status_total_count
ON vulnerability_scans(status, total_count);

CREATE INDEX IF NOT EXISTS idx_vulnerability_ignores_env_created_at
ON vulnerability_ignores(environment_id, created_at);

CREATE INDEX IF NOT EXISTS idx_vulnerability_ignores_env_vulnerability_id
ON vulnerability_ignores(environment_id, vulnerability_id);

DROP INDEX IF EXISTS idx_api_keys_user_id;
DROP INDEX IF EXISTS idx_events_environment_id;
DROP INDEX IF EXISTS idx_image_update_repository;
DROP INDEX IF EXISTS idx_image_update_tag;
DROP INDEX IF EXISTS idx_volume_backups_volume_name;
DROP INDEX IF EXISTS idx_vulnerability_ignores_env;
DROP INDEX IF EXISTS idx_vulnerability_ignores_vuln;
DROP INDEX IF EXISTS idx_vulnerability_scans_status;
