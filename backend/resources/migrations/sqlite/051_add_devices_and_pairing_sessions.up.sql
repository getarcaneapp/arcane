CREATE TABLE devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    api_key_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    device_id TEXT NOT NULL,
    app_version TEXT,
    os_version TEXT,
    device_model TEXT,
    last_seen_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_devices_user_device ON devices(user_id, device_id);
CREATE INDEX idx_devices_user ON devices(user_id);

CREATE TABLE pairing_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    short_code TEXT NOT NULL UNIQUE,
    qr_token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    redeemed_at DATETIME,
    redeemed_device_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (redeemed_device_id) REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX idx_pairing_sessions_expires ON pairing_sessions(expires_at);
