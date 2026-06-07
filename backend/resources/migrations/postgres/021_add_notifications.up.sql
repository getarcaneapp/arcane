CREATE TABLE IF NOT EXISTS notification_settings (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(50) NOT NULL,
    enabled BOOLEAN DEFAULT FALSE,
    config JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notification_settings_provider ON notification_settings(provider);

CREATE TABLE IF NOT EXISTS notification_logs (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(50) NOT NULL,
    image_ref VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    error TEXT,
    metadata JSONB,
    sent_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notification_logs_provider ON notification_logs(provider);
CREATE INDEX idx_notification_logs_sent_at ON notification_logs(sent_at);
