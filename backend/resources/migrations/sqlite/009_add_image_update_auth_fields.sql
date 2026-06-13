-- +goose Up
ALTER TABLE image_updates ADD COLUMN auth_method TEXT;
ALTER TABLE image_updates ADD COLUMN auth_username TEXT;
ALTER TABLE image_updates ADD COLUMN auth_registry TEXT;
ALTER TABLE image_updates ADD COLUMN used_credential INTEGER DEFAULT 0;

-- +goose Down
ALTER TABLE image_updates DROP COLUMN auth_method;
ALTER TABLE image_updates DROP COLUMN auth_username;
ALTER TABLE image_updates DROP COLUMN auth_registry;
ALTER TABLE image_updates DROP COLUMN used_credential;
