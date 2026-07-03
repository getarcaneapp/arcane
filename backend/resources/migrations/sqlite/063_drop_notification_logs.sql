-- +goose Up
-- Notification delivery attempts are recorded in the events table now;
-- nothing reads or writes notification_logs anymore.
DROP TABLE IF EXISTS notification_logs;

-- +goose Down
CREATE TABLE IF NOT EXISTS notification_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider VARCHAR(50) NOT NULL,
    image_ref TEXT NOT NULL,
    status VARCHAR(50) NOT NULL,
    error TEXT,
    metadata TEXT,
    sent_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notification_logs_provider ON notification_logs(provider);
CREATE INDEX idx_notification_logs_sent_at ON notification_logs(sent_at);
