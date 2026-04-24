-- Remove environment bootstrap keys (user_id IS NULL) before restoring the NOT NULL constraint.
DELETE FROM api_keys WHERE user_id IS NULL;
ALTER TABLE api_keys ALTER COLUMN user_id SET NOT NULL;
