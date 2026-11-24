package migrations

import (
	"github.com/pressly/goose/v3"
)

// Two environments can run the same image, so update state is keyed by (image id, environment).
// SQLite cannot alter a primary key in place, so the table is rebuilt with the new key; Postgres
// swaps the constraint. Existing rows belong to the local environment.
const (
	v122SQLiteUp = `
ALTER TABLE image_updates RENAME TO image_updates_previous;

CREATE TABLE image_updates (
    id TEXT NOT NULL,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    has_update BOOLEAN NOT NULL DEFAULT false,
    update_type TEXT,
    current_version TEXT NOT NULL,
    latest_version TEXT,
    current_digest TEXT,
    latest_digest TEXT,
    check_time DATETIME NOT NULL,
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    auth_method TEXT,
    auth_username TEXT,
    auth_registry TEXT,
    used_credential INTEGER DEFAULT 0,
    notification_sent INTEGER DEFAULT 0,
    container_id TEXT NOT NULL DEFAULT '',
    image_id TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    policy_key TEXT NOT NULL DEFAULT '',
    latest_seen_at DATETIME,
    current_created DATETIME,
    latest_created DATETIME,
    blocked_reason TEXT,
    blocked_policy TEXT,
    environment_id TEXT NOT NULL DEFAULT '0',
    PRIMARY KEY (id, environment_id)
);

INSERT INTO image_updates (
    id, repository, tag, has_update, update_type, current_version, latest_version, current_digest,
    latest_digest, check_time, response_time_ms, last_error, created_at, updated_at, auth_method,
    auth_username, auth_registry, used_credential, notification_sent, container_id, image_id,
    project_id, service_name, policy_key, latest_seen_at, current_created, latest_created,
    blocked_reason, blocked_policy, environment_id
)
SELECT
    id, repository, tag, has_update, update_type, current_version, latest_version, current_digest,
    latest_digest, check_time, response_time_ms, last_error, created_at, updated_at, auth_method,
    auth_username, auth_registry, used_credential, notification_sent, container_id, image_id,
    project_id, service_name, policy_key, latest_seen_at, current_created, latest_created,
    blocked_reason, blocked_policy, '0'
FROM image_updates_previous;

DROP TABLE image_updates_previous;

CREATE INDEX idx_image_updates_has_update ON image_updates(has_update);
CREATE INDEX idx_image_updates_has_update_notification ON image_updates(has_update, notification_sent);
CREATE INDEX idx_image_updates_check_time ON image_updates(check_time);
CREATE INDEX idx_image_updates_notification_sent ON image_updates(notification_sent);
CREATE INDEX idx_image_updates_repository_tag ON image_updates(repository, tag);
CREATE INDEX idx_image_updates_environment_id ON image_updates(environment_id);
`
	v122SQLiteDown = `
ALTER TABLE image_updates RENAME TO image_updates_previous;

CREATE TABLE image_updates (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    has_update BOOLEAN NOT NULL DEFAULT false,
    update_type TEXT,
    current_version TEXT NOT NULL,
    latest_version TEXT,
    current_digest TEXT,
    latest_digest TEXT,
    check_time DATETIME NOT NULL,
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    auth_method TEXT,
    auth_username TEXT,
    auth_registry TEXT,
    used_credential INTEGER DEFAULT 0,
    notification_sent INTEGER DEFAULT 0,
    container_id TEXT NOT NULL DEFAULT '',
    image_id TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    policy_key TEXT NOT NULL DEFAULT '',
    latest_seen_at DATETIME,
    current_created DATETIME,
    latest_created DATETIME,
    blocked_reason TEXT,
    blocked_policy TEXT
);

INSERT INTO image_updates (
    id, repository, tag, has_update, update_type, current_version, latest_version, current_digest,
    latest_digest, check_time, response_time_ms, last_error, created_at, updated_at, auth_method,
    auth_username, auth_registry, used_credential, notification_sent, container_id, image_id,
    project_id, service_name, policy_key, latest_seen_at, current_created, latest_created,
    blocked_reason, blocked_policy
)
SELECT
    id, repository, tag, has_update, update_type, current_version, latest_version, current_digest,
    latest_digest, check_time, response_time_ms, last_error, created_at, updated_at, auth_method,
    auth_username, auth_registry, used_credential, notification_sent, container_id, image_id,
    project_id, service_name, policy_key, latest_seen_at, current_created, latest_created,
    blocked_reason, blocked_policy
FROM image_updates_previous
WHERE environment_id = '0';

DROP TABLE image_updates_previous;

CREATE INDEX idx_image_updates_has_update ON image_updates(has_update);
CREATE INDEX idx_image_updates_has_update_notification ON image_updates(has_update, notification_sent);
CREATE INDEX idx_image_updates_check_time ON image_updates(check_time);
CREATE INDEX idx_image_updates_notification_sent ON image_updates(notification_sent);
CREATE INDEX idx_image_updates_repository_tag ON image_updates(repository, tag);
`
	v122PostgresUp = `
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS environment_id text NOT NULL DEFAULT '0';
ALTER TABLE image_updates DROP CONSTRAINT IF EXISTS image_updates_pkey;
ALTER TABLE image_updates ADD PRIMARY KEY (id, environment_id);
CREATE INDEX IF NOT EXISTS idx_image_updates_environment_id ON image_updates(environment_id);
`
	v122PostgresDown = `
DROP INDEX IF EXISTS idx_image_updates_environment_id;
ALTER TABLE image_updates DROP CONSTRAINT IF EXISTS image_updates_pkey;
ALTER TABLE image_updates ADD PRIMARY KEY (id);
ALTER TABLE image_updates DROP COLUMN IF EXISTS environment_id;
`
)

func init() {
	goose.AddMigrationContext(run(v122SQLiteUp, v122PostgresUp), run(v122SQLiteDown, v122PostgresDown))
}
