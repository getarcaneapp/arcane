-- +goose NO TRANSACTION
-- +goose Up
-- Rebuild stale project references left by historical project-table renames.
PRAGMA foreign_keys=OFF;
BEGIN;

CREATE TABLE gitops_syncs_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    branch TEXT NOT NULL,
    compose_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    project_id TEXT,
    auto_sync BOOLEAN NOT NULL DEFAULT false,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    last_sync_at DATETIME,
    last_sync_status TEXT,
    last_sync_error TEXT,
    last_sync_commit TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    sync_directory INTEGER NOT NULL DEFAULT 1,
    synced_files TEXT,
    max_sync_files INTEGER NOT NULL DEFAULT 500,
    max_sync_total_size INTEGER NOT NULL DEFAULT 52428800,
    max_sync_binary_size INTEGER NOT NULL DEFAULT 10485760,
    target_type TEXT NOT NULL DEFAULT 'project',
    pre_deploy_script_path TEXT,
    pre_deploy_runner_image TEXT,
    pre_deploy_env TEXT,
    pre_deploy_extra_mounts TEXT,
    pre_deploy_timeout_sec INTEGER NOT NULL DEFAULT 60,
    pre_deploy_network_mode TEXT NOT NULL DEFAULT 'none',
    pre_deploy_last_run_at DATETIME,
    pre_deploy_last_run_status TEXT,
    pre_deploy_last_run_output TEXT,
    pull_image_after_sync BOOLEAN NOT NULL DEFAULT false,
    redeploy_after_sync BOOLEAN NOT NULL DEFAULT false,
    mode TEXT NOT NULL DEFAULT 'deploy',
    backup_directory TEXT NOT NULL DEFAULT '',
    backup_paths TEXT,
    backup_on_save BOOLEAN NOT NULL DEFAULT true,
    backup_pending BOOLEAN NOT NULL DEFAULT false,
    backup_pending_since DATETIME,
    backup_conflict BOOLEAN NOT NULL DEFAULT false,
    backup_failure_reason TEXT,
    last_backup_at DATETIME,
    last_backup_snapshot TEXT,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE,
    FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

INSERT INTO gitops_syncs_new (
    id,
    name,
    environment_id,
    repository_id,
    branch,
    compose_path,
    project_name,
    project_id,
    auto_sync,
    sync_interval,
    last_sync_at,
    last_sync_status,
    last_sync_error,
    last_sync_commit,
    created_at,
    updated_at,
    sync_directory,
    synced_files,
    max_sync_files,
    max_sync_total_size,
    max_sync_binary_size,
    target_type,
    pre_deploy_script_path,
    pre_deploy_runner_image,
    pre_deploy_env,
    pre_deploy_extra_mounts,
    pre_deploy_timeout_sec,
    pre_deploy_network_mode,
    pre_deploy_last_run_at,
    pre_deploy_last_run_status,
    pre_deploy_last_run_output,
    pull_image_after_sync,
    redeploy_after_sync,
    mode,
    backup_directory,
    backup_paths,
    backup_on_save,
    backup_pending,
    backup_pending_since,
    backup_conflict,
    backup_failure_reason,
    last_backup_at,
    last_backup_snapshot
)
SELECT
    id,
    name,
    environment_id,
    repository_id,
    branch,
    compose_path,
    project_name,
    project_id,
    auto_sync,
    sync_interval,
    last_sync_at,
    last_sync_status,
    last_sync_error,
    last_sync_commit,
    created_at,
    updated_at,
    sync_directory,
    synced_files,
    max_sync_files,
    max_sync_total_size,
    max_sync_binary_size,
    target_type,
    pre_deploy_script_path,
    pre_deploy_runner_image,
    pre_deploy_env,
    pre_deploy_extra_mounts,
    pre_deploy_timeout_sec,
    pre_deploy_network_mode,
    pre_deploy_last_run_at,
    pre_deploy_last_run_status,
    pre_deploy_last_run_output,
    pull_image_after_sync,
    redeploy_after_sync,
    mode,
    backup_directory,
    backup_paths,
    backup_on_save,
    backup_pending,
    backup_pending_since,
    backup_conflict,
    backup_failure_reason,
    last_backup_at,
    last_backup_snapshot
FROM gitops_syncs;

DROP TABLE gitops_syncs;
ALTER TABLE gitops_syncs_new RENAME TO gitops_syncs;

CREATE INDEX idx_gitops_syncs_auto_sync_true
ON gitops_syncs(id, environment_id, sync_interval, last_sync_at)
WHERE auto_sync = 1;
CREATE UNIQUE INDEX idx_gitops_syncs_backup_project ON gitops_syncs(project_id) WHERE mode = 'backup';
CREATE INDEX idx_gitops_syncs_environment_auto_sync
ON gitops_syncs(environment_id, auto_sync);
CREATE INDEX idx_gitops_syncs_environment_id ON gitops_syncs(environment_id);
CREATE INDEX idx_gitops_syncs_environment_last_sync_status
ON gitops_syncs(environment_id, last_sync_status);
CREATE INDEX idx_gitops_syncs_environment_project_id
ON gitops_syncs(environment_id, project_id);
CREATE INDEX idx_gitops_syncs_environment_repository_id
ON gitops_syncs(environment_id, repository_id);
CREATE INDEX idx_gitops_syncs_project_id ON gitops_syncs(project_id);
CREATE INDEX idx_gitops_syncs_repository_id ON gitops_syncs(repository_id);
CREATE INDEX idx_gitops_syncs_sync_directory ON gitops_syncs(sync_directory);

CREATE INDEX idx_gitops_syncs_auto_sync ON gitops_syncs(auto_sync);
CREATE INDEX idx_gitops_syncs_last_sync_commit ON gitops_syncs(last_sync_commit);

COMMIT;
PRAGMA foreign_keys=ON;

-- Keep cascades enabled while repairing authorization records.
BEGIN;
DELETE FROM user_role_assignments
WHERE NOT EXISTS (SELECT 1 FROM users WHERE users.id = user_role_assignments.user_id)
   OR NOT EXISTS (SELECT 1 FROM roles WHERE roles.id = user_role_assignments.role_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = user_role_assignments.environment_id
   ));

DELETE FROM api_key_permissions
WHERE NOT EXISTS (SELECT 1 FROM api_keys WHERE api_keys.id = api_key_permissions.api_key_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = api_key_permissions.environment_id
   ));

DELETE FROM oidc_role_mappings
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE roles.id = oidc_role_mappings.role_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = oidc_role_mappings.environment_id
   ));

DELETE FROM api_keys
WHERE (user_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM users WHERE users.id = api_keys.user_id
   ))
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = api_keys.environment_id
   ));
COMMIT;

-- +goose Down
-- Keep repaired references; deleted orphaned records cannot be reconstructed.
