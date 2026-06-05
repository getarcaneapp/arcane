-- +goose Up
ALTER TABLE users ADD COLUMN locale TEXT;

-- +goose Down
-- SQLite down migration: to rollback, recreate the users table without the 'locale' column and copy data back.
