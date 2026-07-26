-- +goose Up
ALTER TABLE snippets ADD COLUMN target TEXT NOT NULL DEFAULT 'host';
ALTER TABLE snippets ADD COLUMN container_id TEXT;

-- +goose Down
ALTER TABLE snippets DROP COLUMN container_id;
ALTER TABLE snippets DROP COLUMN target;
