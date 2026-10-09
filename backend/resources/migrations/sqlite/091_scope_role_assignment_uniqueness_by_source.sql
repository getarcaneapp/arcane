-- +goose Up
DROP INDEX IF EXISTS idx_ura_uniq;
CREATE UNIQUE INDEX idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), source);

-- +goose Down
-- The old index spans sources, so drop OIDC rows that duplicate a manual grant first.
DELETE FROM user_role_assignments
WHERE source = 'oidc'
  AND EXISTS (
    SELECT 1 FROM user_role_assignments m
    WHERE m.source <> 'oidc'
      AND m.user_id = user_role_assignments.user_id
      AND m.role_id = user_role_assignments.role_id
      AND COALESCE(m.environment_id, '') = COALESCE(user_role_assignments.environment_id, '')
  );
DROP INDEX IF EXISTS idx_ura_uniq;
CREATE UNIQUE INDEX idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''));
