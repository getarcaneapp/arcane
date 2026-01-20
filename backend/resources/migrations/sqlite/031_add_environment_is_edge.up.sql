-- add is_edge column to environments table for edge agent mode
ALTER TABLE environments ADD COLUMN is_edge INTEGER NOT NULL DEFAULT 0;
