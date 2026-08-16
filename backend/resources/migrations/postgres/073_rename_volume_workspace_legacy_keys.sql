-- +goose Up
INSERT INTO settings (key, value)
SELECT 'volumeHelperIdleTimeout', value
FROM settings
WHERE key = 'volumeBrowserHelperIdleTimeout'
ON CONFLICT (key) DO NOTHING;

DELETE FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout';

UPDATE roles
SET permissions = (
    SELECT jsonb_agg(permission ORDER BY permission)
    FROM (
        SELECT DISTINCT CASE value
            WHEN 'volumes:browse' THEN 'volumes:read'
            ELSE value
        END AS permission
        FROM jsonb_array_elements_text(roles.permissions)
    ) AS migrated_permissions
)
WHERE permissions ? 'volumes:browse';

DELETE FROM api_key_permissions AS legacy
WHERE legacy.permission = 'volumes:browse'
  AND EXISTS (
      SELECT 1
      FROM api_key_permissions AS current
      WHERE current.api_key_id = legacy.api_key_id
        AND current.permission = 'volumes:read'
        AND COALESCE(current.environment_id, '') = COALESCE(legacy.environment_id, '')
  );

UPDATE api_key_permissions
SET permission = 'volumes:read'
WHERE permission = 'volumes:browse';

-- +goose Down
INSERT INTO settings (key, value)
SELECT 'volumeBrowserHelperIdleTimeout', value
FROM settings
WHERE key = 'volumeHelperIdleTimeout'
ON CONFLICT (key) DO NOTHING;

DELETE FROM settings WHERE key = 'volumeHelperIdleTimeout';

-- Permission grants are intentionally not reversed because volumes:read may
-- have existed before this migration and cannot be distinguished safely.
