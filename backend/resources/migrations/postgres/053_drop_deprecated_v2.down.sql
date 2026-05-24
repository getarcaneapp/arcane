-- v2.0.0 rollback: recreate the apprise_settings table shape (data is not restored).
CREATE TABLE IF NOT EXISTS apprise_settings (
    id SERIAL PRIMARY KEY,
    api_url TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    image_update_tag VARCHAR(255),
    container_update_tag VARCHAR(255),
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);
