ALTER TABLE container_registries ADD COLUMN registry_type TEXT NOT NULL DEFAULT 'generic';
ALTER TABLE container_registries ADD COLUMN aws_access_key_id TEXT;
ALTER TABLE container_registries ADD COLUMN aws_secret_access_key TEXT;
ALTER TABLE container_registries ADD COLUMN aws_region TEXT;
ALTER TABLE container_registries ADD COLUMN ecr_token TEXT;
ALTER TABLE container_registries ADD COLUMN ecr_token_generated_at TIMESTAMPTZ;
