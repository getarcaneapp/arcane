-- +goose Up
ALTER TABLE users ADD COLUMN has_avatar BOOLEAN NOT NULL DEFAULT 0;

CREATE TABLE user_avatars (
    user_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    mime_type TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS user_avatars;
ALTER TABLE users DROP COLUMN has_avatar;
