CREATE TABLE IF NOT EXISTS apprise_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_url TEXT NOT NULL,
    enabled INTEGER DEFAULT 0,
    image_update_tag TEXT,
    container_update_tag TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
