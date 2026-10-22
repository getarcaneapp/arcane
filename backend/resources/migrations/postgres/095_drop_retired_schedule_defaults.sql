-- +goose Up
DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval', 'autoHealInterval') AND value = '*/30 * * * * *';

-- +goose Down
-- Deleted rows held retired defaults; the current defaults apply without them.
