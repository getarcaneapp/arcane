-- +goose Up
DELETE FROM user_role_assignments
WHERE NOT EXISTS (SELECT 1 FROM users WHERE users.id = user_role_assignments.user_id)
   OR NOT EXISTS (SELECT 1 FROM roles WHERE roles.id = user_role_assignments.role_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = user_role_assignments.environment_id
   ));

DELETE FROM api_key_permissions
WHERE NOT EXISTS (SELECT 1 FROM api_keys WHERE api_keys.id = api_key_permissions.api_key_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = api_key_permissions.environment_id
   ));

DELETE FROM oidc_role_mappings
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE roles.id = oidc_role_mappings.role_id)
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = oidc_role_mappings.environment_id
   ));

DELETE FROM api_keys
WHERE (user_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM users WHERE users.id = api_keys.user_id
   ))
   OR (environment_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM environments WHERE environments.id = api_keys.environment_id
   ));

-- +goose Down
-- Deleted orphaned records cannot be reconstructed; restore them from a pre-upgrade backup.
