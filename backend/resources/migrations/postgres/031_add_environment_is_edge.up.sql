-- add is_edge column to environments table for edge agent mode
ALTER TABLE environments ADD COLUMN is_edge BOOLEAN NOT NULL DEFAULT false;
