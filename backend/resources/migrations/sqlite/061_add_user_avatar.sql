-- +goose Up
ALTER TABLE users ADD COLUMN avatar_data BLOB;
ALTER TABLE users ADD COLUMN avatar_mime_type TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_data;
ALTER TABLE users DROP COLUMN avatar_mime_type;
