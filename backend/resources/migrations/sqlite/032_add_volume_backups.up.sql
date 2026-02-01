CREATE TABLE volume_backups (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP
);
CREATE INDEX idx_volume_backups_volume_name ON volume_backups(volume_name);
