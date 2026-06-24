-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS has_avatar BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS user_avatars (
    user_id TEXT PRIMARY KEY,
    data BYTEA NOT NULL,
    mime_type TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS user_avatars;
ALTER TABLE users DROP COLUMN IF EXISTS has_avatar;
