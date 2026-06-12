-- +goose Up
ALTER TABLE projects ADD COLUMN status_reason TEXT;

-- +goose Down
ALTER TABLE projects DROP COLUMN status_reason;
