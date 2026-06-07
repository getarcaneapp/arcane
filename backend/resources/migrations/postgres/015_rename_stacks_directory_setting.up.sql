BEGIN;
INSERT INTO settings (key, value)
SELECT 'projectsDirectory', value
FROM settings
WHERE key = 'stacksDirectory'
ON CONFLICT (key) DO NOTHING;

DELETE FROM settings WHERE key = 'stacksDirectory';
COMMIT;