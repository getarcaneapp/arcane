-- Add api_key_id column to environments table for API key-based pairing
ALTER TABLE environments ADD COLUMN api_key_id TEXT REFERENCES api_keys(id) ON DELETE SET NULL;

-- Add environment_id column to api_keys table to link API keys to environments
ALTER TABLE api_keys ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE;
