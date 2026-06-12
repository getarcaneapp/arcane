-- +goose Up
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS registry_type TEXT NOT NULL DEFAULT 'generic';
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_access_key_id TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_secret_access_key TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS aws_region TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS ecr_token TEXT;
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS ecr_token_generated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE container_registries DROP COLUMN IF EXISTS registry_type;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_access_key_id;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_secret_access_key;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_region;
ALTER TABLE container_registries DROP COLUMN IF EXISTS ecr_token;
ALTER TABLE container_registries DROP COLUMN IF EXISTS ecr_token_generated_at;
