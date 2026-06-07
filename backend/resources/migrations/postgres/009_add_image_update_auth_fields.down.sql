ALTER TABLE IF EXISTS image_updates
    DROP COLUMN IF EXISTS used_credential,
    DROP COLUMN IF EXISTS auth_registry,
    DROP COLUMN IF EXISTS auth_username,
    DROP COLUMN IF EXISTS auth_method;