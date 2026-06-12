-- +goose Up
INSERT OR IGNORE INTO settings (key, value)
SELECT 'projectsDirectory', value
FROM settings
WHERE key = 'stacksDirectory';

DELETE FROM settings WHERE key = 'stacksDirectory';

-- +goose Down
INSERT OR IGNORE INTO settings (key, value)
SELECT 'stacksDirectory', value
FROM settings
WHERE key = 'projectsDirectory';

DELETE FROM settings WHERE key = 'projectsDirectory';
