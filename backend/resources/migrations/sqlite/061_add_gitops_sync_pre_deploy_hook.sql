-- +goose Up
-- Add pre-deploy lifecycle hook fields to gitops_syncs table.
-- See backend/internal/models/gitops_sync.go for field semantics.
-- pre_deploy_script_path: path inside the synced directory to a script
--   executed in a throwaway container before each deploy of the linked project.
-- pre_deploy_runner_image: image used to run the script (required when script path is set).
-- pre_deploy_env: newline-separated KEY=VALUE pairs exposed to the script as env vars.
-- pre_deploy_extra_mounts: newline-separated src:tgt[:ro|:rw] bind mounts.
-- pre_deploy_timeout_sec: hard timeout for script execution.
-- pre_deploy_network_mode: Docker network mode for the runner container; "none" denies outbound.
-- pre_deploy_last_run_*: last-run state, written by the lifecycle service.
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_script_path TEXT;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_runner_image TEXT;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_env TEXT;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_extra_mounts TEXT;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_timeout_sec INTEGER NOT NULL DEFAULT 60;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_network_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_last_run_at DATETIME;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_last_run_status TEXT;
ALTER TABLE gitops_syncs ADD COLUMN pre_deploy_last_run_output TEXT;

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_last_run_output;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_last_run_status;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_last_run_at;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_network_mode;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_timeout_sec;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_extra_mounts;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_env;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_runner_image;
ALTER TABLE gitops_syncs DROP COLUMN pre_deploy_script_path;
