BEGIN;
-- Drop new FK
ALTER TABLE containers
  DROP CONSTRAINT IF EXISTS containers_project_id_fkey;

-- Rename column back
ALTER TABLE containers
  RENAME COLUMN IF EXISTS project_id TO stack_id;

-- Optionally restore FK to stacks if it exists
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='stacks' AND table_schema = current_schema()) THEN
    ALTER TABLE containers
      ADD CONSTRAINT containers_stack_id_fkey
      FOREIGN KEY (stack_id) REFERENCES stacks(id) ON DELETE SET NULL;
  END IF;
END$$;
COMMIT;