-- +goose Up
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS latest_seen_at TIMESTAMPTZ;
UPDATE image_updates SET latest_seen_at = CURRENT_TIMESTAMP WHERE has_update = true;

-- +goose Down
ALTER TABLE image_updates DROP COLUMN IF EXISTS latest_seen_at;
