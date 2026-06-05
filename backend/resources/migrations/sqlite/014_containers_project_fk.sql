-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=off;
ALTER TABLE containers RENAME COLUMN stack_id TO project_id;
PRAGMA foreign_keys=on;

-- +goose Down
PRAGMA foreign_keys=off;
ALTER TABLE containers RENAME COLUMN project_id TO stack_id;
PRAGMA foreign_keys=on;
