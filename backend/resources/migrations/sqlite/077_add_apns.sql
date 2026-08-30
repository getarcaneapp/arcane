-- +goose Up
CREATE TABLE IF NOT EXISTS apns_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    events TEXT NOT NULL DEFAULT '{}',
    environment_ids TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    last_seen_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_apns_devices_recipient ON apns_devices(recipient_id);
CREATE INDEX IF NOT EXISTS idx_apns_devices_user ON apns_devices(user_id);

CREATE TABLE IF NOT EXISTS apns_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    envelope TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_apns_outbox_next_attempt ON apns_outbox(next_attempt_at);

-- +goose Down
DROP INDEX IF EXISTS idx_apns_outbox_next_attempt;
DROP TABLE IF EXISTS apns_outbox;
DROP INDEX IF EXISTS idx_apns_devices_user;
DROP INDEX IF EXISTS idx_apns_devices_recipient;
DROP TABLE IF EXISTS apns_devices;
