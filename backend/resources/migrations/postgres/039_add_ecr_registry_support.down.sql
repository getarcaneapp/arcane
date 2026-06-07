ALTER TABLE container_registries DROP COLUMN IF EXISTS registry_type;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_access_key_id;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_secret_access_key;
ALTER TABLE container_registries DROP COLUMN IF EXISTS aws_region;
ALTER TABLE container_registries DROP COLUMN IF EXISTS ecr_token;
ALTER TABLE container_registries DROP COLUMN IF EXISTS ecr_token_generated_at;
