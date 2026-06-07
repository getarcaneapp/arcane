BEGIN;
INSERT INTO settings (key, value)
SELECT 'stacksDirectory', value
FROM settings
WHERE key = 'projectsDirectory'
ON CONFLICT (key) DO NOTHING;

DELETE FROM settings WHERE key = 'projectsDirectory';
COMMIT;