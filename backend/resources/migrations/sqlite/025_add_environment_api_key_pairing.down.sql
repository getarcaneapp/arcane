-- Remove environment_id column from api_keys table
ALTER TABLE api_keys DROP COLUMN environment_id;

-- Remove api_key_id column from environments table
ALTER TABLE environments DROP COLUMN api_key_id;
