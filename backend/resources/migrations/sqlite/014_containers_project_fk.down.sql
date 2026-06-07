PRAGMA foreign_keys=off;
ALTER TABLE containers RENAME COLUMN project_id TO stack_id;
PRAGMA foreign_keys=on;