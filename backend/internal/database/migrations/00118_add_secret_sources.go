package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v118SecretSourcePermissions = `["secret-sources:list","secret-sources:read","secret-sources:create","secret-sources:update","secret-sources:delete","secret-sources:refresh"]`
	v118PriorPermissions        = `[
    "users:list", "users:read", "users:create", "users:update", "users:delete", "roles:list", "roles:read",
    "apikeys:list", "apikeys:read", "apikeys:create", "apikeys:update", "apikeys:delete", "federated:list",
    "federated:read", "federated:create", "federated:update", "federated:delete", "settings:read",
    "settings:update", "environments:list", "environments:read", "environments:create",
    "environments:update", "environments:delete", "environments:pair", "environments:sync",
    "environments:restart", "registries:list", "registries:read", "registries:create", "registries:update",
    "registries:delete", "registries:test", "registries:browse", "registries:delete-tags", "templates:list",
    "templates:read", "templates:create", "templates:update", "templates:delete", "variables:list",
    "variables:read", "variables:create", "variables:update", "variables:delete", "variables:sync",
    "git-repositories:list", "git-repositories:read", "git-repositories:create", "git-repositories:update",
    "git-repositories:delete", "git-repositories:test", "git-repositories:sync", "s3-destinations:list",
    "s3-destinations:read", "s3-destinations:create", "s3-destinations:update", "s3-destinations:delete",
    "s3-destinations:test", "s3-destinations:sync", "system-backups:list", "system-backups:read",
    "system-backups:create", "system-backups:update", "system-backups:delete", "system-backups:upload",
    "system-backups:restore", "system-backups:recovery-key", "events:list", "events:delete",
    "diagnostics:read", "diagnostics:profile", "containers:list", "containers:read", "containers:logs",
    "containers:create", "containers:start", "containers:stop", "containers:restart", "containers:redeploy",
    "containers:update", "containers:kill", "containers:pause", "containers:delete", "containers:exec",
    "containers:autoupdate", "projects:list", "projects:read", "projects:logs", "projects:create",
    "projects:update", "projects:deploy", "projects:jobs:run", "projects:down", "projects:restart", "projects:delete",
    "projects:archive", "images:list", "images:read", "images:pull", "images:push", "images:build",
    "images:tag", "images:commit", "images:prune", "images:delete", "images:upload", "images:patch",
    "volumes:list", "volumes:read", "volumes:create", "volumes:rename", "volumes:delete", "volumes:prune",
    "volumes:upload", "volumes:backup", "networks:list", "networks:read", "networks:create",
    "networks:delete", "networks:prune", "networks:connect", "networks:disconnect", "swarm:read",
    "swarm:update", "swarm:init", "swarm:join", "swarm:leave", "swarm:unlock", "swarm-services:list",
    "swarm-services:read", "swarm-services:create", "swarm-services:update", "swarm-services:delete",
    "swarm-services:logs", "swarm-stacks:list", "swarm-stacks:read", "swarm-stacks:create",
    "swarm-stacks:update", "swarm-stacks:delete", "swarm-nodes:list", "swarm-nodes:read",
    "swarm-nodes:update", "swarm-nodes:delete", "swarm-nodes:promote", "swarm-nodes:demote",
    "swarm-configs:list", "swarm-configs:read", "swarm-configs:create", "swarm-configs:update",
    "swarm-configs:delete", "swarm-secrets:list", "swarm-secrets:read", "swarm-secrets:create",
    "swarm-secrets:update", "swarm-secrets:delete", "podman:read", "podman:autoupdate", "podman-pods:list",
    "podman-pods:read", "podman-pods:create", "podman-pods:delete", "podman-pods:prune", "podman-pods:start",
    "podman-pods:stop", "podman-pods:restart", "podman-pods:pause", "podman-pods:kill",
    "podman-quadlets:list", "podman-quadlets:read", "podman-quadlets:create", "podman-quadlets:update",
    "podman-quadlets:delete", "podman-quadlets:start", "podman-quadlets:stop", "podman-quadlets:restart",
    "podman-secrets:list", "podman-secrets:read", "podman-secrets:create", "podman-secrets:update",
    "podman-secrets:delete", "podman-kube:read", "podman-kube:create", "podman-kube:delete", "gitops:list",
    "gitops:read", "gitops:create", "gitops:update", "gitops:delete", "gitops:sync", "gitops:lifecycle",
    "gitops:backup", "gitops:bidirectional", "webhooks:list", "webhooks:create", "webhooks:update",
    "webhooks:delete", "jobs:list", "jobs:read", "jobs:update", "jobs:run", "jobs:restart", "jobs:retry",
    "jobs:cancel", "jobs:ack", "notifications:list", "notifications:read", "notifications:update",
    "notifications:delete", "notifications:test", "system:read", "system:prune", "system:upgrade",
    "image-updates:list", "image-updates:read", "image-updates:check", "image-updates:apply",
    "vulnerabilities:list", "vulnerabilities:read", "vulnerabilities:scan", "vulnerabilities:update",
    "build-workspaces:list", "build-workspaces:read", "build-workspaces:create", "build-workspaces:delete",
    "activities:list", "activities:read", "activities:cancel", "activities:delete",
    "environment-settings:read", "environment-settings:update"
]`

	v118SQLiteUp = `
CREATE TABLE secret_sources (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    preset_id TEXT NOT NULL DEFAULT '',
    definition TEXT NOT NULL,
    inputs TEXT NOT NULL DEFAULT '{}',
    credentials TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    key_prefix TEXT NOT NULL DEFAULT '',
    all_environments BOOLEAN NOT NULL DEFAULT TRUE,
    inject_into_containers BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_fetched_at DATETIME,
    last_error TEXT NOT NULL DEFAULT '',
    last_key_count INTEGER NOT NULL DEFAULT 0,
    last_skipped_key_count INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_secret_sources_name ON secret_sources (name);
CREATE TABLE secret_source_environments (
    secret_source_id TEXT NOT NULL REFERENCES secret_sources(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    PRIMARY KEY (secret_source_id, environment_id)
);
CREATE INDEX idx_secret_source_environments_env ON secret_source_environments (environment_id);
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM (
        SELECT value FROM json_each(roles.permissions)
        UNION
        SELECT value FROM json_each('` + v118SecretSourcePermissions + `')
    )
)
WHERE NOT EXISTS (
    SELECT 1 FROM json_each('` + v118PriorPermissions + `') AS prior
    WHERE prior.value NOT IN (SELECT value FROM json_each(roles.permissions))
);
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    admins.api_key_id,
    granted.value,
    admins.environment_id,
    CURRENT_TIMESTAMP
FROM (
    SELECT api_key_id, MIN(environment_id) AS environment_id
    FROM api_key_permissions
    WHERE COALESCE(environment_id, '') = ''
        AND permission IN (SELECT value FROM json_each('` + v118PriorPermissions + `'))
    GROUP BY api_key_id
    HAVING COUNT(DISTINCT permission) = (SELECT COUNT(*) FROM json_each('` + v118PriorPermissions + `'))
) AS admins
CROSS JOIN json_each('` + v118SecretSourcePermissions + `') AS granted
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = admins.api_key_id
        AND existing.permission = granted.value
        AND COALESCE(existing.environment_id, '') = ''
);
`
	v118PostgresUp = `
CREATE TABLE IF NOT EXISTS secret_sources (
    id text PRIMARY KEY,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    name text NOT NULL,
    kind text NOT NULL,
    preset_id text NOT NULL DEFAULT '',
    definition text NOT NULL,
    inputs text NOT NULL DEFAULT '{}',
    credentials text NOT NULL DEFAULT '',
    path text NOT NULL DEFAULT '',
    key_prefix text NOT NULL DEFAULT '',
    all_environments boolean NOT NULL DEFAULT true,
    inject_into_containers boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    last_fetched_at timestamp with time zone,
    last_error text NOT NULL DEFAULT '',
    last_key_count integer NOT NULL DEFAULT 0,
    last_skipped_key_count integer NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_sources_name ON secret_sources (name);
CREATE TABLE IF NOT EXISTS secret_source_environments (
    secret_source_id text NOT NULL REFERENCES secret_sources(id) ON DELETE CASCADE,
    environment_id text NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    PRIMARY KEY (secret_source_id, environment_id)
);
CREATE INDEX IF NOT EXISTS idx_secret_source_environments_env ON secret_source_environments (environment_id);
UPDATE roles
SET permissions = permissions || (
    SELECT COALESCE(jsonb_agg(granted), '[]'::jsonb)
    FROM jsonb_array_elements_text('` + v118SecretSourcePermissions + `'::jsonb) AS granted
    WHERE NOT roles.permissions @> jsonb_build_array(granted)
)
WHERE roles.permissions @> '` + v118PriorPermissions + `'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, admins.api_key_id, granted, admins.environment_id, CURRENT_TIMESTAMP
FROM (
    SELECT api_key_id, MIN(environment_id) AS environment_id
    FROM api_key_permissions
    WHERE COALESCE(environment_id, '') = ''
        AND permission IN (SELECT jsonb_array_elements_text('` + v118PriorPermissions + `'::jsonb))
    GROUP BY api_key_id
    HAVING COUNT(DISTINCT permission) = jsonb_array_length('` + v118PriorPermissions + `'::jsonb)
) AS admins
CROSS JOIN jsonb_array_elements_text('` + v118SecretSourcePermissions + `'::jsonb) AS granted
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = admins.api_key_id
        AND existing.permission = granted
        AND COALESCE(existing.environment_id, '') = ''
);
`
	v118SQLiteDown = `
DROP TABLE secret_source_environments;
DROP TABLE secret_sources;
DELETE FROM webhooks WHERE target_type = 'secretsource';
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value NOT LIKE 'secret-sources:%'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value LIKE 'secret-sources:%');
DELETE FROM api_key_permissions WHERE permission LIKE 'secret-sources:%';
`
	v118PostgresDown = `
DROP TABLE IF EXISTS secret_source_environments;
DROP TABLE IF EXISTS secret_sources;
DELETE FROM webhooks WHERE target_type = 'secretsource';
UPDATE roles
SET permissions = (
    SELECT COALESCE(jsonb_agg(value), '[]'::jsonb)
    FROM jsonb_array_elements_text(roles.permissions) AS value
    WHERE value NOT LIKE 'secret-sources:%'
)
WHERE EXISTS (SELECT 1 FROM jsonb_array_elements_text(roles.permissions) AS value WHERE value LIKE 'secret-sources:%');
DELETE FROM api_key_permissions WHERE permission LIKE 'secret-sources:%';
`
)

func init() {
	goose.AddMigrationContext(run(v118SQLiteUp, v118PostgresUp), run(v118SQLiteDown, v118PostgresDown))
}
