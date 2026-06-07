INSERT OR IGNORE INTO settings (key, value)
SELECT 'projectsDirectory', value
FROM settings
WHERE key = 'stacksDirectory';

DELETE FROM settings WHERE key = 'stacksDirectory';