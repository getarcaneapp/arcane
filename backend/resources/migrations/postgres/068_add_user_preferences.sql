-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferences TEXT;

-- Seed every user's preferences from the current global settings so that
-- upgrading does not visually change anything. The old settings rows are left
-- in place; PruneUnknownSettings removes them on first startup of the new
-- binary, which keeps this migration reversible.
UPDATE users SET preferences = jsonb_build_object(
  'applicationTheme',           (SELECT value FROM settings WHERE key = 'applicationTheme'),
  'accentColor',                (SELECT value FROM settings WHERE key = 'accentColor'),
  'iconCatalog',                (SELECT value FROM settings WHERE key = 'iconCatalog'),
  'defaultLandingPage',         (SELECT value FROM settings WHERE key = 'defaultLandingPage'),
  'mobileNavigationMode',       (SELECT value FROM settings WHERE key = 'mobileNavigationMode'),
  'oledMode',                   (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'oledMode'),
  'glassEffectsEnabled',        (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'glassEffectsEnabled'),
  'animationsEnabled',          (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'animationsEnabled'),
  'sidebarHoverExpansion',      (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'sidebarHoverExpansion'),
  'keyboardShortcutsEnabled',   (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'keyboardShortcutsEnabled'),
  'mobileNavigationShowLabels', (SELECT CASE value WHEN 'true' THEN to_jsonb(TRUE) WHEN 'false' THEN to_jsonb(FALSE) END
                                   FROM settings WHERE key = 'mobileNavigationShowLabels')
)::text;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS preferences;
