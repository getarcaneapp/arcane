-- SQLite doesn't support dropping foreign key constraints directly
-- We need to recreate the table without the constraint

PRAGMA foreign_keys=off;

CREATE TABLE events_new (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    title TEXT NOT NULL,
    description TEXT,
    resource_type TEXT,
    resource_id TEXT,
    resource_name TEXT,
    user_id TEXT,
    username TEXT,
    environment_id TEXT,
    metadata JSON,
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

INSERT INTO events_new SELECT * FROM events;
DROP TABLE events;
ALTER TABLE events_new RENAME TO events;

-- Recreate indexes
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_severity ON events(severity);
CREATE INDEX IF NOT EXISTS idx_events_resource_type ON events(resource_type);
CREATE INDEX IF NOT EXISTS idx_events_resource_id ON events(resource_id);
CREATE INDEX IF NOT EXISTS idx_events_user_id ON events(user_id);
CREATE INDEX IF NOT EXISTS idx_events_environment_id ON events(environment_id);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);

PRAGMA foreign_keys=on;