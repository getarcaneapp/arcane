PRAGMA foreign_keys=off;
ALTER TABLE containers RENAME COLUMN stack_id TO project_id;
PRAGMA foreign_keys=on;