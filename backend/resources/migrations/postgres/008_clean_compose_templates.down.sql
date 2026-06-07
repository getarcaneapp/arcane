ALTER TABLE compose_templates
  ADD COLUMN IF NOT EXISTS meta_updated_at TEXT;