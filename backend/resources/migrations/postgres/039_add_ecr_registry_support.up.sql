ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS registry_type TEXT NOT NULL DEFAULT 'generic';
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_access_key_id TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_secret_access_key TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_region TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS ecr_token TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS ecr_token_generated_at TIMESTAMPTZ;
