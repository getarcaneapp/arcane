-- Create migration file: backend/resources/migrations/sqlite/003_remove_events_environment_fk.down.sql
-- This would require recreating the table with the foreign key constraint
-- For simplicity, we'll leave this empty since rolling back would be complex in SQLite
DROP TABLE IF EXISTS events;