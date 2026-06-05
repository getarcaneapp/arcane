-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE cname text;
BEGIN
  SELECT tc.constraint_name INTO cname
  FROM information_schema.table_constraints tc
  JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_name = kcu.table_name
  WHERE tc.table_name = 'containers' AND tc.constraint_type = 'FOREIGN KEY' AND kcu.column_name = 'stack_id'
    AND tc.table_schema = current_schema() AND kcu.table_schema = current_schema();
  IF cname IS NOT NULL THEN
    EXECUTE format('ALTER TABLE containers DROP CONSTRAINT %I', cname);
  END IF;
END$$;
-- +goose StatementEnd

ALTER TABLE containers
  RENAME COLUMN stack_id TO project_id;

-- Create FK to projects
ALTER TABLE containers
  ADD CONSTRAINT containers_project_id_fkey
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

-- +goose Down
-- Drop new FK
ALTER TABLE containers
  DROP CONSTRAINT IF EXISTS containers_project_id_fkey;

-- Rename column back
ALTER TABLE containers
  RENAME COLUMN IF EXISTS project_id TO stack_id;

-- Optionally restore FK to stacks if it exists
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='stacks' AND table_schema = current_schema()) THEN
    ALTER TABLE containers
      ADD CONSTRAINT containers_stack_id_fkey
      FOREIGN KEY (stack_id) REFERENCES stacks(id) ON DELETE SET NULL;
  END IF;
END$$;
-- +goose StatementEnd
