
DELETE FROM settings WHERE key = 'oidcGroupsClaim';

DROP INDEX IF EXISTS idx_orm_claim;
DROP TABLE IF EXISTS oidc_role_mappings;

DROP INDEX IF EXISTS idx_akp_uniq;
DROP INDEX IF EXISTS idx_akp_key;
DROP TABLE IF EXISTS api_key_permissions;

DROP INDEX IF EXISTS idx_ura_uniq;
DROP INDEX IF EXISTS idx_ura_env;
DROP INDEX IF EXISTS idx_ura_role;
DROP INDEX IF EXISTS idx_ura_user;
DROP TABLE IF EXISTS user_role_assignments;

DROP TABLE IF EXISTS roles;
