-- Recreate user_sessions_table (matches initial schema)
CREATE TABLE IF NOT EXISTS user_sessions_table (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_accessed DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    is_active BOOLEAN NOT NULL DEFAULT true,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users_table(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions_table(user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_token ON user_sessions_table(token);