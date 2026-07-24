-- +goose Up
ALTER TABLE users ADD COLUMN preferences TEXT;

-- Seed every user's preferences from the current global settings so that
-- upgrading does not visually change anything. The old settings rows are left
-- in place; PruneUnknownSettings removes them on first startup of the new
-- binary, which keeps this migration reversible.
UPDATE users SET preferences = json_object(
  'applicationTheme',           (SELECT value FROM settings WHERE key = 'applicationTheme'),
  'accentColor',                (SELECT value FROM settings WHERE key = 'accentColor'),
  'iconCatalog',                (SELECT value FROM settings WHERE key = 'iconCatalog'),
  'defaultLandingPage',         (SELECT value FROM settings WHERE key = 'defaultLandingPage'),
  'mobileNavigationMode',       (SELECT value FROM settings WHERE key = 'mobileNavigationMode'),
  'oledMode',                   json(CASE (SELECT value FROM settings WHERE key = 'oledMode')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END),
  'glassEffectsEnabled',        json(CASE (SELECT value FROM settings WHERE key = 'glassEffectsEnabled')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END),
  'animationsEnabled',          json(CASE (SELECT value FROM settings WHERE key = 'animationsEnabled')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END),
  'sidebarHoverExpansion',      json(CASE (SELECT value FROM settings WHERE key = 'sidebarHoverExpansion')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END),
  'keyboardShortcutsEnabled',   json(CASE (SELECT value FROM settings WHERE key = 'keyboardShortcutsEnabled')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END),
  'mobileNavigationShowLabels', json(CASE (SELECT value FROM settings WHERE key = 'mobileNavigationShowLabels')
                                       WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE 'null' END)
);

-- +goose Down
ALTER TABLE users DROP COLUMN preferences;
