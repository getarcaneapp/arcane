-- +goose Up
-- v2.0.0: drop the Apprise notification service table and deprecated settings rows.
DROP TABLE IF EXISTS apprise_settings;

DELETE FROM settings WHERE key IN (
    'dockerPruneMode',
    'scheduledPruneContainers',
    'scheduledPruneImages',
    'scheduledPruneVolumes',
    'scheduledPruneNetworks',
    'scheduledPruneBuildCache',
    'authOidcConfig'
);

-- +goose Down
-- v2.0.0 rollback: recreate the apprise_settings table shape (data is not restored).
CREATE TABLE IF NOT EXISTS apprise_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_url TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    image_update_tag VARCHAR(255),
    container_update_tag VARCHAR(255),
    created_at DATETIME,
    updated_at DATETIME
);
