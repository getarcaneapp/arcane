-- +goose Up
ALTER TABLE image_updates ADD COLUMN latest_seen_at DATETIME;
UPDATE image_updates SET latest_seen_at = CURRENT_TIMESTAMP WHERE has_update = 1;

-- +goose Down
ALTER TABLE image_updates DROP COLUMN latest_seen_at;
