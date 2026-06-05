-- +goose Up
ALTER TABLE image_updates ADD COLUMN auth_method TEXT;
ALTER TABLE image_updates ADD COLUMN auth_username TEXT;
ALTER TABLE image_updates ADD COLUMN auth_registry TEXT;
ALTER TABLE image_updates ADD COLUMN used_credential INTEGER DEFAULT 0;

-- +goose Down
-- no-op: dropping columns in SQLite requires table rebuild; intentionally left empty
