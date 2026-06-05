-- +goose Up
-- Add ssh_host_key_verification column to git_repositories table
-- Values: 'strict' (use known_hosts), 'accept_new' (default - auto-add unknown hosts), 'skip' (disable verification)
ALTER TABLE git_repositories ADD COLUMN ssh_host_key_verification TEXT NOT NULL DEFAULT 'accept_new';

-- +goose Down
-- SQLite doesn't support DROP COLUMN directly, but we can recreate the table
-- For simplicity, we'll just leave the column in place (it's harmless)
-- In production, you'd want to recreate the table without this column
