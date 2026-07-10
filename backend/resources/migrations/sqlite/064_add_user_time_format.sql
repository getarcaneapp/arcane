-- +goose Up
ALTER TABLE users ADD COLUMN time_format TEXT NOT NULL DEFAULT 'auto'
    CHECK (time_format IN ('auto', '12h', '24h'));

-- +goose Down
ALTER TABLE users DROP COLUMN time_format;
