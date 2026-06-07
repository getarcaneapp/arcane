-- Drop indexes first (safe if they exist), then drop the table
DROP INDEX IF EXISTS idx_user_sessions_token;
DROP INDEX IF EXISTS idx_user_sessions_user_id;

DROP TABLE IF EXISTS user_sessions_table;