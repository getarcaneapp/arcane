-- Backfill missing vulnerability scan settings defaults
INSERT OR IGNORE INTO settings (key, value) VALUES ('vulnerabilityScanEnabled', 'false');
UPDATE settings
SET value = 'false'
WHERE key = 'vulnerabilityScanEnabled'
  AND (value IS NULL OR TRIM(value) = '');

INSERT OR IGNORE INTO settings (key, value) VALUES ('vulnerabilityScanInterval', '0 0 0 * * *');
UPDATE settings
SET value = '0 0 0 * * *'
WHERE key = 'vulnerabilityScanInterval'
  AND (value IS NULL OR TRIM(value) = '');

-- Backfill missing auto-update excluded containers setting
INSERT OR IGNORE INTO settings (key, value) VALUES ('autoUpdateExcludedContainers', '');
UPDATE settings
SET value = ''
WHERE key = 'autoUpdateExcludedContainers'
  AND value IS NULL;
