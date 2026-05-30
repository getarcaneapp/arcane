DROP INDEX IF EXISTS idx_user_sessions_federated_credential_id;
ALTER TABLE user_sessions DROP COLUMN federated_credential_id;
ALTER TABLE user_sessions DROP COLUMN source;
ALTER TABLE users DROP COLUMN is_service_account;

DROP INDEX IF EXISTS idx_federated_credentials_role_id;
DROP INDEX IF EXISTS idx_federated_credentials_identity_user_id;
DROP INDEX IF EXISTS idx_federated_credentials_enabled;
DROP INDEX IF EXISTS idx_federated_credentials_issuer_url;
DROP INDEX IF EXISTS idx_federated_token_replays_expires_at;
DROP INDEX IF EXISTS idx_federated_token_replays_issuer_url;
DROP TABLE IF EXISTS federated_token_replays;
DROP TABLE IF EXISTS federated_credentials;
