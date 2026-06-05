-- +goose Up
-- Environment bootstrap keys are owned by the system, not a user.
-- Allow user_id to be NULL so agent-side key creation doesn't violate the FK constraint.
ALTER TABLE api_keys ALTER COLUMN user_id DROP NOT NULL;

-- +goose Down
-- Remove environment bootstrap keys (user_id IS NULL) before restoring the NOT NULL constraint.
DELETE FROM api_keys WHERE user_id IS NULL;
ALTER TABLE api_keys ALTER COLUMN user_id SET NOT NULL;
