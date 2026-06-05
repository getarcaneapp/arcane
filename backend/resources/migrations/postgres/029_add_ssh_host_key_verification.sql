-- +goose Up
-- Add ssh_host_key_verification column to git_repositories table
-- Values: 'strict' (use known_hosts), 'accept_new' (default - auto-add unknown hosts), 'skip' (disable verification)
ALTER TABLE git_repositories ADD COLUMN IF NOT EXISTS ssh_host_key_verification TEXT NOT NULL DEFAULT 'accept_new';

-- +goose Down
ALTER TABLE git_repositories DROP COLUMN IF EXISTS ssh_host_key_verification;
