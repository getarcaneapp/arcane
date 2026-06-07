INSERT OR IGNORE INTO settings (key, value)
SELECT 'stacksDirectory', value
FROM settings
WHERE key = 'projectsDirectory';

DELETE FROM settings WHERE key = 'projectsDirectory';