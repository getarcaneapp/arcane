-- Add mobile_dock_tabs column to users table to store customizable mobile dock preferences
ALTER TABLE users ADD COLUMN mobile_dock_tabs TEXT DEFAULT '["/dashboard","/projects","/containers","/images"]';
