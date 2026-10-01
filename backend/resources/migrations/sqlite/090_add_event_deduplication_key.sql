-- +goose Up
ALTER TABLE events ADD COLUMN deduplication_key TEXT;
CREATE UNIQUE INDEX idx_events_deduplication_key ON events (deduplication_key);

-- +goose Down
DROP INDEX idx_events_deduplication_key;
ALTER TABLE events DROP COLUMN deduplication_key;
