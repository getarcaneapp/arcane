-- Environment bootstrap keys are owned by the system, not a user.
-- Allow user_id to be NULL so agent-side key creation doesn't violate the FK constraint.
ALTER TABLE api_keys ALTER COLUMN user_id DROP NOT NULL;
