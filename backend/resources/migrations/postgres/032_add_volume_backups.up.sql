CREATE TABLE volume_backups (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    size BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_volume_backups_volume_name ON volume_backups(volume_name);
