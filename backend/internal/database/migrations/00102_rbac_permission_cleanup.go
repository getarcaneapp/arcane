package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v102CleanupSQLiteUp = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'customize:manage'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'customize:manage');
DELETE FROM api_key_permissions WHERE permission = 'customize:manage';
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'image-updates:apply')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'image-updates:check')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'image-updates:apply');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'image-updates:apply',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'image-updates:check'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'image-updates:apply'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v102CleanupPostgresUp = `
UPDATE roles SET permissions = permissions - 'customize:manage' WHERE permissions @> '["customize:manage"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'customize:manage';
UPDATE roles SET permissions = permissions || '["image-updates:apply"]'::jsonb
WHERE permissions @> '["image-updates:check"]'::jsonb AND NOT permissions @> '["image-updates:apply"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'image-updates:apply', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'image-updates:check'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'image-updates:apply'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v102CleanupSQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'image-updates:apply'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'image-updates:apply');
DELETE FROM api_key_permissions WHERE permission = 'image-updates:apply';
`
	v102CleanupPostgresDown = `
UPDATE roles SET permissions = permissions - 'image-updates:apply' WHERE permissions @> '["image-updates:apply"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'image-updates:apply';
`
	v102SQLiteCreateMap = `
CREATE TEMP TABLE rbac_permission_map (
    old_permission TEXT NOT NULL,
    new_permission TEXT NOT NULL
);
`
	v102PostgresCreateMap = `
CREATE TEMP TABLE rbac_permission_map (
    ordinal SERIAL,
    old_permission TEXT NOT NULL,
    new_permission TEXT NOT NULL
);
`
	v102RenameMap = `
INSERT INTO rbac_permission_map (old_permission, new_permission) VALUES
    ('settings:write', 'settings:update'),
    ('settings:write', 'environment-settings:update'),
    ('settings:read', 'settings:read'),
    ('settings:read', 'environment-settings:read'),
    ('settings:update', 'settings:update'),
    ('settings:update', 'environment-settings:update'),
    ('containers:edit', 'containers:update'),
    ('variables:read', 'variables:list'),
    ('variables:read', 'variables:read'),
    ('system-backups:read', 'system-backups:list'),
    ('system-backups:read', 'system-backups:read'),
    ('system-backups:manage', 'system-backups:create'),
    ('system-backups:manage', 'system-backups:update'),
    ('system-backups:manage', 'system-backups:delete'),
    ('system-backups:manage', 'system-backups:upload'),
    ('events:read', 'events:list'),
    ('notifications:manage', 'notifications:list'),
    ('notifications:manage', 'notifications:read'),
    ('notifications:manage', 'notifications:update'),
    ('notifications:manage', 'notifications:delete'),
    ('notifications:manage', 'notifications:test'),
    ('jobs:manage', 'jobs:list'),
    ('jobs:manage', 'jobs:read'),
    ('jobs:manage', 'jobs:update'),
    ('jobs:manage', 'jobs:run'),
    ('jobs:manage', 'jobs:restart'),
    ('jobs:manage', 'jobs:retry'),
    ('jobs:manage', 'jobs:cancel'),
    ('jobs:manage', 'jobs:ack'),
    ('swarm:spec', 'swarm:update'),
    ('swarm:read', 'swarm:read'),
    ('swarm:read', 'swarm-services:list'),
    ('swarm:read', 'swarm-services:read'),
    ('swarm:read', 'swarm-stacks:list'),
    ('swarm:read', 'swarm-stacks:read'),
    ('swarm:read', 'swarm-nodes:list'),
    ('swarm:read', 'swarm-nodes:read'),
    ('swarm:read', 'swarm-configs:list'),
    ('swarm:read', 'swarm-configs:read'),
    ('swarm:read', 'swarm-secrets:list'),
    ('swarm:read', 'swarm-secrets:read'),
    ('swarm:services', 'swarm-services:create'),
    ('swarm:services', 'swarm-services:update'),
    ('swarm:services', 'swarm-services:delete'),
    ('swarm:services:logs', 'swarm-services:logs'),
    ('swarm:stacks', 'swarm-stacks:read'),
    ('swarm:stacks', 'swarm-stacks:create'),
    ('swarm:stacks', 'swarm-stacks:update'),
    ('swarm:stacks', 'swarm-stacks:delete'),
    ('swarm:nodes', 'swarm-nodes:update'),
    ('swarm:nodes', 'swarm-nodes:delete'),
    ('swarm:nodes', 'swarm-nodes:promote'),
    ('swarm:nodes', 'swarm-nodes:demote'),
    ('swarm:configs', 'swarm-configs:create'),
    ('swarm:configs', 'swarm-configs:update'),
    ('swarm:configs', 'swarm-configs:delete'),
    ('swarm:secrets', 'swarm-secrets:create'),
    ('swarm:secrets', 'swarm-secrets:update'),
    ('swarm:secrets', 'swarm-secrets:delete'),
    ('podman:read', 'podman:read'),
    ('podman:read', 'podman-pods:list'),
    ('podman:read', 'podman-pods:read'),
    ('podman:read', 'podman-quadlets:list'),
    ('podman:read', 'podman-quadlets:read'),
    ('podman:read', 'podman-secrets:list'),
    ('podman:read', 'podman-secrets:read'),
    ('podman:pods', 'podman-pods:create'),
    ('podman:pods', 'podman-pods:delete'),
    ('podman:pods', 'podman-pods:prune'),
    ('podman:pods', 'podman-pods:start'),
    ('podman:pods', 'podman-pods:stop'),
    ('podman:pods', 'podman-pods:restart'),
    ('podman:pods', 'podman-pods:pause'),
    ('podman:pods', 'podman-pods:kill'),
    ('podman:quadlets', 'podman-quadlets:create'),
    ('podman:quadlets', 'podman-quadlets:update'),
    ('podman:quadlets', 'podman-quadlets:delete'),
    ('podman:quadlets:lifecycle', 'podman-quadlets:start'),
    ('podman:quadlets:lifecycle', 'podman-quadlets:stop'),
    ('podman:quadlets:lifecycle', 'podman-quadlets:restart'),
    ('podman:kube', 'podman-kube:read'),
    ('podman:kube', 'podman-kube:create'),
    ('podman:kube', 'podman-kube:delete'),
    ('podman:secrets', 'podman-secrets:create'),
    ('podman:secrets', 'podman-secrets:update'),
    ('podman:secrets', 'podman-secrets:delete'),
    ('image-updates:read', 'image-updates:list'),
    ('image-updates:read', 'image-updates:read'),
    ('vulnerabilities:read', 'vulnerabilities:list'),
    ('vulnerabilities:read', 'vulnerabilities:read'),
    ('vulnerabilities:manage', 'vulnerabilities:update'),
    ('build-workspaces:manage', 'build-workspaces:list'),
    ('build-workspaces:manage', 'build-workspaces:read'),
    ('build-workspaces:manage', 'build-workspaces:create'),
    ('build-workspaces:manage', 'build-workspaces:delete'),
    ('activities:read', 'activities:list'),
    ('activities:read', 'activities:read');
`
	v102RequiresMap = `
DELETE FROM rbac_permission_map;
INSERT INTO rbac_permission_map (old_permission, new_permission) VALUES
    ('users:list', 'users:list'),
    ('users:read', 'users:read'),
    ('users:create', 'users:create'),
    ('users:update', 'users:update'),
    ('users:delete', 'users:delete'),
    ('roles:list', 'roles:list'),
    ('roles:read', 'roles:read'),
    ('apikeys:list', 'apikeys:list'),
    ('apikeys:read', 'apikeys:read'),
    ('apikeys:create', 'apikeys:create'),
    ('apikeys:update', 'apikeys:update'),
    ('apikeys:delete', 'apikeys:delete'),
    ('federated:list', 'federated:list'),
    ('federated:read', 'federated:read'),
    ('federated:create', 'federated:create'),
    ('federated:update', 'federated:update'),
    ('federated:delete', 'federated:delete'),
    ('settings:read', 'settings:read'),
    ('settings:update', 'settings:update'),
    ('environments:list', 'environments:list'),
    ('environments:read', 'environments:read'),
    ('environments:create', 'environments:create'),
    ('environments:update', 'environments:update'),
    ('environments:delete', 'environments:delete'),
    ('environments:pair', 'environments:pair'),
    ('environments:sync', 'environments:sync'),
    ('environments:restart', 'environments:restart'),
    ('registries:list', 'registries:list'),
    ('registries:read', 'registries:read'),
    ('registries:create', 'registries:create'),
    ('registries:update', 'registries:update'),
    ('registries:delete', 'registries:delete'),
    ('registries:test', 'registries:test'),
    ('registries:browse', 'registries:browse'),
    ('registries:delete-tags', 'registries:delete-tags'),
    ('templates:list', 'templates:list'),
    ('templates:read', 'templates:read'),
    ('templates:create', 'templates:create'),
    ('templates:update', 'templates:update'),
    ('templates:delete', 'templates:delete'),
    ('variables:list', 'variables:list'),
    ('variables:read', 'variables:read'),
    ('variables:create', 'variables:create'),
    ('variables:update', 'variables:update'),
    ('variables:delete', 'variables:delete'),
    ('variables:sync', 'variables:sync'),
    ('git-repositories:list', 'git-repositories:list'),
    ('git-repositories:read', 'git-repositories:read'),
    ('git-repositories:create', 'git-repositories:create'),
    ('git-repositories:update', 'git-repositories:update'),
    ('git-repositories:delete', 'git-repositories:delete'),
    ('git-repositories:test', 'git-repositories:test'),
    ('git-repositories:sync', 'git-repositories:sync'),
    ('s3-destinations:list', 's3-destinations:list'),
    ('s3-destinations:read', 's3-destinations:read'),
    ('s3-destinations:create', 's3-destinations:create'),
    ('s3-destinations:update', 's3-destinations:update'),
    ('s3-destinations:delete', 's3-destinations:delete'),
    ('s3-destinations:test', 's3-destinations:test'),
    ('s3-destinations:sync', 's3-destinations:sync'),
    ('system-backups:list', 'system-backups:list'),
    ('system-backups:read', 'system-backups:read'),
    ('system-backups:create', 'system-backups:create'),
    ('system-backups:update', 'system-backups:update'),
    ('system-backups:delete', 'system-backups:delete'),
    ('system-backups:upload', 'system-backups:upload'),
    ('system-backups:restore', 'system-backups:restore'),
    ('system-backups:recovery-key', 'system-backups:recovery-key'),
    ('events:list', 'events:list'),
    ('events:delete', 'events:delete'),
    ('diagnostics:read', 'diagnostics:read'),
    ('diagnostics:profile', 'diagnostics:profile'),
    ('containers:list', 'containers:list'),
    ('containers:read', 'containers:read'),
    ('containers:logs', 'containers:logs'),
    ('containers:create', 'containers:create'),
    ('containers:start', 'containers:start'),
    ('containers:stop', 'containers:stop'),
    ('containers:restart', 'containers:restart'),
    ('containers:redeploy', 'containers:redeploy'),
    ('containers:update', 'containers:update'),
    ('containers:kill', 'containers:kill'),
    ('containers:pause', 'containers:pause'),
    ('containers:delete', 'containers:delete'),
    ('containers:exec', 'containers:exec'),
    ('containers:autoupdate', 'containers:autoupdate'),
    ('projects:list', 'projects:list'),
    ('projects:read', 'projects:read'),
    ('projects:logs', 'projects:logs'),
    ('projects:create', 'projects:create'),
    ('projects:update', 'projects:update'),
    ('projects:deploy', 'projects:deploy'),
    ('projects:down', 'projects:down'),
    ('projects:restart', 'projects:restart'),
    ('projects:delete', 'projects:delete'),
    ('projects:archive', 'projects:archive'),
    ('images:list', 'images:list'),
    ('images:read', 'images:read'),
    ('images:pull', 'images:pull'),
    ('images:push', 'images:push'),
    ('images:build', 'images:build'),
    ('images:tag', 'images:tag'),
    ('images:commit', 'images:commit'),
    ('images:prune', 'images:prune'),
    ('images:delete', 'images:delete'),
    ('images:upload', 'images:upload'),
    ('images:patch', 'images:patch'),
    ('volumes:list', 'volumes:list'),
    ('volumes:read', 'volumes:read'),
    ('volumes:create', 'volumes:create'),
    ('volumes:rename', 'volumes:rename'),
    ('volumes:delete', 'volumes:delete'),
    ('volumes:prune', 'volumes:prune'),
    ('volumes:upload', 'volumes:upload'),
    ('volumes:backup', 'volumes:backup'),
    ('networks:list', 'networks:list'),
    ('networks:read', 'networks:read'),
    ('networks:create', 'networks:create'),
    ('networks:delete', 'networks:delete'),
    ('networks:prune', 'networks:prune'),
    ('networks:connect', 'networks:connect'),
    ('networks:disconnect', 'networks:disconnect'),
    ('swarm:read', 'swarm:read'),
    ('swarm:update', 'swarm:update'),
    ('swarm:init', 'swarm:init'),
    ('swarm:join', 'swarm:join'),
    ('swarm:leave', 'swarm:leave'),
    ('swarm:unlock', 'swarm:unlock'),
    ('swarm-services:list', 'swarm-services:list'),
    ('swarm-services:read', 'swarm-services:read'),
    ('swarm-services:create', 'swarm-services:create'),
    ('swarm-services:update', 'swarm-services:update'),
    ('swarm-services:delete', 'swarm-services:delete'),
    ('swarm-services:logs', 'swarm-services:logs'),
    ('swarm-stacks:list', 'swarm-stacks:list'),
    ('swarm-stacks:read', 'swarm-stacks:read'),
    ('swarm-stacks:create', 'swarm-stacks:create'),
    ('swarm-stacks:update', 'swarm-stacks:update'),
    ('swarm-stacks:delete', 'swarm-stacks:delete'),
    ('swarm-nodes:list', 'swarm-nodes:list'),
    ('swarm-nodes:read', 'swarm-nodes:read'),
    ('swarm-nodes:update', 'swarm-nodes:update'),
    ('swarm-nodes:delete', 'swarm-nodes:delete'),
    ('swarm-nodes:promote', 'swarm-nodes:promote'),
    ('swarm-nodes:demote', 'swarm-nodes:demote'),
    ('swarm-configs:list', 'swarm-configs:list'),
    ('swarm-configs:read', 'swarm-configs:read'),
    ('swarm-configs:create', 'swarm-configs:create'),
    ('swarm-configs:update', 'swarm-configs:update'),
    ('swarm-configs:delete', 'swarm-configs:delete'),
    ('swarm-secrets:list', 'swarm-secrets:list'),
    ('swarm-secrets:read', 'swarm-secrets:read'),
    ('swarm-secrets:create', 'swarm-secrets:create'),
    ('swarm-secrets:update', 'swarm-secrets:update'),
    ('swarm-secrets:delete', 'swarm-secrets:delete'),
    ('podman:read', 'podman:read'),
    ('podman:autoupdate', 'podman:autoupdate'),
    ('podman-pods:list', 'podman-pods:list'),
    ('podman-pods:read', 'podman-pods:read'),
    ('podman-pods:create', 'podman-pods:create'),
    ('podman-pods:delete', 'podman-pods:delete'),
    ('podman-pods:prune', 'podman-pods:prune'),
    ('podman-pods:start', 'podman-pods:start'),
    ('podman-pods:stop', 'podman-pods:stop'),
    ('podman-pods:restart', 'podman-pods:restart'),
    ('podman-pods:pause', 'podman-pods:pause'),
    ('podman-pods:kill', 'podman-pods:kill'),
    ('podman-quadlets:list', 'podman-quadlets:list'),
    ('podman-quadlets:read', 'podman-quadlets:read'),
    ('podman-quadlets:create', 'podman-quadlets:create'),
    ('podman-quadlets:update', 'podman-quadlets:update'),
    ('podman-quadlets:delete', 'podman-quadlets:delete'),
    ('podman-quadlets:start', 'podman-quadlets:start'),
    ('podman-quadlets:stop', 'podman-quadlets:stop'),
    ('podman-quadlets:restart', 'podman-quadlets:restart'),
    ('podman-secrets:list', 'podman-secrets:list'),
    ('podman-secrets:read', 'podman-secrets:read'),
    ('podman-secrets:create', 'podman-secrets:create'),
    ('podman-secrets:update', 'podman-secrets:update'),
    ('podman-secrets:delete', 'podman-secrets:delete'),
    ('podman-kube:read', 'podman-kube:read'),
    ('podman-kube:create', 'podman-kube:create'),
    ('podman-kube:delete', 'podman-kube:delete'),
    ('gitops:list', 'gitops:list'),
    ('gitops:read', 'gitops:read'),
    ('gitops:create', 'gitops:create'),
    ('gitops:update', 'gitops:update'),
    ('gitops:delete', 'gitops:delete'),
    ('gitops:sync', 'gitops:sync'),
    ('gitops:lifecycle', 'gitops:lifecycle'),
    ('gitops:backup', 'gitops:backup'),
    ('webhooks:list', 'webhooks:list'),
    ('webhooks:create', 'webhooks:create'),
    ('webhooks:update', 'webhooks:update'),
    ('webhooks:delete', 'webhooks:delete'),
    ('jobs:list', 'jobs:list'),
    ('jobs:read', 'jobs:read'),
    ('jobs:update', 'jobs:update'),
    ('jobs:run', 'jobs:run'),
    ('jobs:restart', 'jobs:restart'),
    ('jobs:retry', 'jobs:retry'),
    ('jobs:cancel', 'jobs:cancel'),
    ('jobs:ack', 'jobs:ack'),
    ('notifications:list', 'notifications:list'),
    ('notifications:read', 'notifications:read'),
    ('notifications:update', 'notifications:update'),
    ('notifications:delete', 'notifications:delete'),
    ('notifications:test', 'notifications:test'),
    ('system:read', 'system:read'),
    ('system:prune', 'system:prune'),
    ('system:upgrade', 'system:upgrade'),
    ('image-updates:list', 'image-updates:list'),
    ('image-updates:read', 'image-updates:read'),
    ('image-updates:check', 'image-updates:check'),
    ('image-updates:apply', 'image-updates:apply'),
    ('vulnerabilities:list', 'vulnerabilities:list'),
    ('vulnerabilities:read', 'vulnerabilities:read'),
    ('vulnerabilities:scan', 'vulnerabilities:scan'),
    ('vulnerabilities:update', 'vulnerabilities:update'),
    ('build-workspaces:list', 'build-workspaces:list'),
    ('build-workspaces:read', 'build-workspaces:read'),
    ('build-workspaces:create', 'build-workspaces:create'),
    ('build-workspaces:delete', 'build-workspaces:delete'),
    ('activities:list', 'activities:list'),
    ('activities:read', 'activities:read'),
    ('activities:cancel', 'activities:cancel'),
    ('activities:delete', 'activities:delete'),
    ('environment-settings:read', 'environment-settings:read'),
    ('environment-settings:update', 'environment-settings:update'),
    ('users:read', 'users:list'),
    ('users:create', 'users:list'),
    ('users:create', 'users:read'),
    ('users:update', 'users:list'),
    ('users:update', 'users:read'),
    ('users:delete', 'users:list'),
    ('users:delete', 'users:read'),
    ('roles:read', 'roles:list'),
    ('apikeys:read', 'apikeys:list'),
    ('apikeys:create', 'apikeys:list'),
    ('apikeys:create', 'apikeys:read'),
    ('apikeys:update', 'apikeys:list'),
    ('apikeys:update', 'apikeys:read'),
    ('apikeys:delete', 'apikeys:list'),
    ('apikeys:delete', 'apikeys:read'),
    ('federated:read', 'federated:list'),
    ('federated:create', 'federated:list'),
    ('federated:create', 'federated:read'),
    ('federated:update', 'federated:list'),
    ('federated:update', 'federated:read'),
    ('federated:delete', 'federated:list'),
    ('federated:delete', 'federated:read'),
    ('settings:update', 'settings:read'),
    ('environments:read', 'environments:list'),
    ('environments:create', 'environments:list'),
    ('environments:create', 'environments:read'),
    ('environments:update', 'environments:list'),
    ('environments:update', 'environments:read'),
    ('environments:delete', 'environments:list'),
    ('environments:delete', 'environments:read'),
    ('environments:pair', 'environments:list'),
    ('environments:pair', 'environments:read'),
    ('environments:sync', 'environments:list'),
    ('environments:sync', 'environments:read'),
    ('environments:restart', 'environments:list'),
    ('environments:restart', 'environments:read'),
    ('registries:read', 'registries:list'),
    ('registries:create', 'registries:list'),
    ('registries:create', 'registries:read'),
    ('registries:update', 'registries:list'),
    ('registries:update', 'registries:read'),
    ('registries:delete', 'registries:list'),
    ('registries:delete', 'registries:read'),
    ('registries:test', 'registries:list'),
    ('registries:test', 'registries:read'),
    ('registries:browse', 'registries:list'),
    ('registries:browse', 'registries:read'),
    ('registries:delete-tags', 'registries:list'),
    ('registries:delete-tags', 'registries:read'),
    ('registries:delete-tags', 'registries:browse'),
    ('templates:read', 'templates:list'),
    ('templates:create', 'templates:list'),
    ('templates:create', 'templates:read'),
    ('templates:update', 'templates:list'),
    ('templates:update', 'templates:read'),
    ('templates:delete', 'templates:list'),
    ('templates:delete', 'templates:read'),
    ('variables:read', 'variables:list'),
    ('variables:create', 'variables:list'),
    ('variables:create', 'variables:read'),
    ('variables:update', 'variables:list'),
    ('variables:update', 'variables:read'),
    ('variables:delete', 'variables:list'),
    ('variables:delete', 'variables:read'),
    ('variables:sync', 'variables:list'),
    ('variables:sync', 'variables:read'),
    ('git-repositories:read', 'git-repositories:list'),
    ('git-repositories:create', 'git-repositories:list'),
    ('git-repositories:create', 'git-repositories:read'),
    ('git-repositories:update', 'git-repositories:list'),
    ('git-repositories:update', 'git-repositories:read'),
    ('git-repositories:delete', 'git-repositories:list'),
    ('git-repositories:delete', 'git-repositories:read'),
    ('git-repositories:test', 'git-repositories:list'),
    ('git-repositories:test', 'git-repositories:read'),
    ('git-repositories:sync', 'git-repositories:list'),
    ('git-repositories:sync', 'git-repositories:read'),
    ('s3-destinations:read', 's3-destinations:list'),
    ('s3-destinations:create', 's3-destinations:list'),
    ('s3-destinations:create', 's3-destinations:read'),
    ('s3-destinations:update', 's3-destinations:list'),
    ('s3-destinations:update', 's3-destinations:read'),
    ('s3-destinations:delete', 's3-destinations:list'),
    ('s3-destinations:delete', 's3-destinations:read'),
    ('s3-destinations:test', 's3-destinations:list'),
    ('s3-destinations:test', 's3-destinations:read'),
    ('s3-destinations:sync', 's3-destinations:list'),
    ('s3-destinations:sync', 's3-destinations:read'),
    ('system-backups:read', 'system-backups:list'),
    ('system-backups:create', 'system-backups:list'),
    ('system-backups:create', 'system-backups:read'),
    ('system-backups:update', 'system-backups:list'),
    ('system-backups:update', 'system-backups:read'),
    ('system-backups:delete', 'system-backups:list'),
    ('system-backups:delete', 'system-backups:read'),
    ('system-backups:upload', 's3-destinations:list'),
    ('system-backups:upload', 'system-backups:list'),
    ('system-backups:upload', 'system-backups:read'),
    ('system-backups:restore', 'system-backups:list'),
    ('system-backups:restore', 'system-backups:read'),
    ('system-backups:recovery-key', 'system-backups:list'),
    ('system-backups:recovery-key', 'system-backups:read'),
    ('events:delete', 'events:list'),
    ('diagnostics:profile', 'diagnostics:read'),
    ('containers:read', 'containers:list'),
    ('containers:logs', 'containers:list'),
    ('containers:logs', 'containers:read'),
    ('containers:create', 'containers:list'),
    ('containers:create', 'containers:read'),
    ('containers:create', 'images:list'),
    ('containers:create', 'images:read'),
    ('containers:create', 'volumes:list'),
    ('containers:create', 'networks:list'),
    ('containers:start', 'containers:list'),
    ('containers:start', 'containers:read'),
    ('containers:start', 'activities:list'),
    ('containers:start', 'activities:read'),
    ('containers:stop', 'containers:list'),
    ('containers:stop', 'containers:read'),
    ('containers:stop', 'activities:list'),
    ('containers:stop', 'activities:read'),
    ('containers:restart', 'containers:list'),
    ('containers:restart', 'containers:read'),
    ('containers:restart', 'activities:list'),
    ('containers:restart', 'activities:read'),
    ('containers:redeploy', 'containers:list'),
    ('containers:redeploy', 'containers:read'),
    ('containers:redeploy', 'activities:list'),
    ('containers:redeploy', 'activities:read'),
    ('containers:update', 'containers:list'),
    ('containers:update', 'containers:read'),
    ('containers:update', 'images:list'),
    ('containers:update', 'images:read'),
    ('containers:update', 'volumes:list'),
    ('containers:update', 'networks:list'),
    ('containers:update', 'activities:list'),
    ('containers:update', 'activities:read'),
    ('containers:kill', 'containers:list'),
    ('containers:kill', 'containers:read'),
    ('containers:kill', 'activities:list'),
    ('containers:kill', 'activities:read'),
    ('containers:pause', 'containers:list'),
    ('containers:pause', 'containers:read'),
    ('containers:pause', 'activities:list'),
    ('containers:pause', 'activities:read'),
    ('containers:delete', 'containers:list'),
    ('containers:delete', 'containers:read'),
    ('containers:delete', 'activities:list'),
    ('containers:delete', 'activities:read'),
    ('containers:exec', 'containers:list'),
    ('containers:exec', 'containers:read'),
    ('containers:autoupdate', 'containers:list'),
    ('containers:autoupdate', 'containers:read'),
    ('projects:read', 'projects:list'),
    ('projects:logs', 'projects:list'),
    ('projects:logs', 'projects:read'),
    ('projects:create', 'projects:list'),
    ('projects:create', 'projects:read'),
    ('projects:create', 'activities:list'),
    ('projects:create', 'activities:read'),
    ('projects:update', 'projects:list'),
    ('projects:update', 'projects:read'),
    ('projects:update', 'activities:list'),
    ('projects:update', 'activities:read'),
    ('projects:deploy', 'projects:list'),
    ('projects:deploy', 'projects:read'),
    ('projects:deploy', 'activities:list'),
    ('projects:deploy', 'activities:read'),
    ('projects:down', 'projects:list'),
    ('projects:down', 'projects:read'),
    ('projects:down', 'activities:list'),
    ('projects:down', 'activities:read'),
    ('projects:restart', 'projects:list'),
    ('projects:restart', 'projects:read'),
    ('projects:restart', 'activities:list'),
    ('projects:restart', 'activities:read'),
    ('projects:delete', 'projects:list'),
    ('projects:delete', 'projects:read'),
    ('projects:delete', 'activities:list'),
    ('projects:delete', 'activities:read'),
    ('projects:archive', 'projects:list'),
    ('projects:archive', 'projects:read'),
    ('images:read', 'images:list'),
    ('images:pull', 'images:list'),
    ('images:pull', 'images:read'),
    ('images:pull', 'activities:list'),
    ('images:pull', 'activities:read'),
    ('images:push', 'images:list'),
    ('images:push', 'images:read'),
    ('images:build', 'images:list'),
    ('images:build', 'images:read'),
    ('images:build', 'build-workspaces:list'),
    ('images:build', 'activities:list'),
    ('images:build', 'activities:read'),
    ('images:tag', 'images:list'),
    ('images:tag', 'images:read'),
    ('images:commit', 'containers:list'),
    ('images:commit', 'containers:read'),
    ('images:commit', 'images:list'),
    ('images:commit', 'images:read'),
    ('images:prune', 'images:list'),
    ('images:prune', 'images:read'),
    ('images:delete', 'images:list'),
    ('images:delete', 'images:read'),
    ('images:upload', 'images:list'),
    ('images:upload', 'images:read'),
    ('images:patch', 'images:list'),
    ('images:patch', 'images:read'),
    ('images:patch', 'vulnerabilities:list'),
    ('images:patch', 'activities:list'),
    ('images:patch', 'activities:read'),
    ('volumes:read', 'volumes:list'),
    ('volumes:create', 'volumes:list'),
    ('volumes:create', 'volumes:read'),
    ('volumes:create', 'activities:list'),
    ('volumes:create', 'activities:read'),
    ('volumes:rename', 'volumes:list'),
    ('volumes:rename', 'volumes:read'),
    ('volumes:rename', 'activities:list'),
    ('volumes:rename', 'activities:read'),
    ('volumes:delete', 'volumes:list'),
    ('volumes:delete', 'volumes:read'),
    ('volumes:delete', 'activities:list'),
    ('volumes:delete', 'activities:read'),
    ('volumes:prune', 'volumes:list'),
    ('volumes:prune', 'volumes:read'),
    ('volumes:prune', 'activities:list'),
    ('volumes:prune', 'activities:read'),
    ('volumes:upload', 'volumes:list'),
    ('volumes:upload', 'volumes:read'),
    ('volumes:upload', 'activities:list'),
    ('volumes:upload', 'activities:read'),
    ('volumes:backup', 'volumes:list'),
    ('volumes:backup', 'volumes:read'),
    ('volumes:backup', 'activities:list'),
    ('volumes:backup', 'activities:read'),
    ('networks:read', 'networks:list'),
    ('networks:create', 'networks:list'),
    ('networks:create', 'networks:read'),
    ('networks:create', 'activities:list'),
    ('networks:create', 'activities:read'),
    ('networks:delete', 'networks:list'),
    ('networks:delete', 'networks:read'),
    ('networks:delete', 'activities:list'),
    ('networks:delete', 'activities:read'),
    ('networks:prune', 'networks:list'),
    ('networks:prune', 'networks:read'),
    ('networks:prune', 'activities:list'),
    ('networks:prune', 'activities:read'),
    ('networks:connect', 'networks:list'),
    ('networks:connect', 'networks:read'),
    ('networks:connect', 'activities:list'),
    ('networks:connect', 'activities:read'),
    ('networks:disconnect', 'networks:list'),
    ('networks:disconnect', 'networks:read'),
    ('networks:disconnect', 'activities:list'),
    ('networks:disconnect', 'activities:read'),
    ('swarm:update', 'swarm:read'),
    ('swarm:init', 'swarm:read'),
    ('swarm:join', 'swarm:read'),
    ('swarm:leave', 'swarm:read'),
    ('swarm:unlock', 'swarm:read'),
    ('swarm-services:read', 'swarm-services:list'),
    ('swarm-services:create', 'swarm-services:list'),
    ('swarm-services:create', 'swarm-services:read'),
    ('swarm-services:update', 'swarm-services:list'),
    ('swarm-services:update', 'swarm-services:read'),
    ('swarm-services:delete', 'swarm-services:list'),
    ('swarm-services:delete', 'swarm-services:read'),
    ('swarm-services:logs', 'swarm-services:list'),
    ('swarm-services:logs', 'swarm-services:read'),
    ('swarm-stacks:read', 'swarm-stacks:list'),
    ('swarm-stacks:create', 'swarm-stacks:list'),
    ('swarm-stacks:create', 'swarm-stacks:read'),
    ('swarm-stacks:update', 'swarm-stacks:list'),
    ('swarm-stacks:update', 'swarm-stacks:read'),
    ('swarm-stacks:delete', 'swarm-stacks:list'),
    ('swarm-stacks:delete', 'swarm-stacks:read'),
    ('swarm-nodes:read', 'swarm-nodes:list'),
    ('swarm-nodes:update', 'swarm-nodes:list'),
    ('swarm-nodes:update', 'swarm-nodes:read'),
    ('swarm-nodes:delete', 'swarm-nodes:list'),
    ('swarm-nodes:delete', 'swarm-nodes:read'),
    ('swarm-nodes:promote', 'swarm-nodes:list'),
    ('swarm-nodes:promote', 'swarm-nodes:read'),
    ('swarm-nodes:demote', 'swarm-nodes:list'),
    ('swarm-nodes:demote', 'swarm-nodes:read'),
    ('swarm-configs:read', 'swarm-configs:list'),
    ('swarm-configs:create', 'swarm-configs:list'),
    ('swarm-configs:create', 'swarm-configs:read'),
    ('swarm-configs:update', 'swarm-configs:list'),
    ('swarm-configs:update', 'swarm-configs:read'),
    ('swarm-configs:delete', 'swarm-configs:list'),
    ('swarm-configs:delete', 'swarm-configs:read'),
    ('swarm-secrets:read', 'swarm-secrets:list'),
    ('swarm-secrets:create', 'swarm-secrets:list'),
    ('swarm-secrets:create', 'swarm-secrets:read'),
    ('swarm-secrets:update', 'swarm-secrets:list'),
    ('swarm-secrets:update', 'swarm-secrets:read'),
    ('swarm-secrets:delete', 'swarm-secrets:list'),
    ('swarm-secrets:delete', 'swarm-secrets:read'),
    ('podman:autoupdate', 'podman:read'),
    ('podman-pods:read', 'podman-pods:list'),
    ('podman-pods:create', 'podman-pods:list'),
    ('podman-pods:create', 'podman-pods:read'),
    ('podman-pods:delete', 'podman-pods:list'),
    ('podman-pods:delete', 'podman-pods:read'),
    ('podman-pods:prune', 'podman-pods:list'),
    ('podman-pods:prune', 'podman-pods:read'),
    ('podman-pods:start', 'podman-pods:list'),
    ('podman-pods:start', 'podman-pods:read'),
    ('podman-pods:stop', 'podman-pods:list'),
    ('podman-pods:stop', 'podman-pods:read'),
    ('podman-pods:restart', 'podman-pods:list'),
    ('podman-pods:restart', 'podman-pods:read'),
    ('podman-pods:pause', 'podman-pods:list'),
    ('podman-pods:pause', 'podman-pods:read'),
    ('podman-pods:kill', 'podman-pods:list'),
    ('podman-pods:kill', 'podman-pods:read'),
    ('podman-quadlets:read', 'podman-quadlets:list'),
    ('podman-quadlets:create', 'podman-quadlets:list'),
    ('podman-quadlets:create', 'podman-quadlets:read'),
    ('podman-quadlets:update', 'podman-quadlets:list'),
    ('podman-quadlets:update', 'podman-quadlets:read'),
    ('podman-quadlets:delete', 'podman-quadlets:list'),
    ('podman-quadlets:delete', 'podman-quadlets:read'),
    ('podman-quadlets:start', 'podman-quadlets:list'),
    ('podman-quadlets:start', 'podman-quadlets:read'),
    ('podman-quadlets:stop', 'podman-quadlets:list'),
    ('podman-quadlets:stop', 'podman-quadlets:read'),
    ('podman-quadlets:restart', 'podman-quadlets:list'),
    ('podman-quadlets:restart', 'podman-quadlets:read'),
    ('podman-secrets:read', 'podman-secrets:list'),
    ('podman-secrets:create', 'podman-secrets:list'),
    ('podman-secrets:create', 'podman-secrets:read'),
    ('podman-secrets:update', 'podman-secrets:list'),
    ('podman-secrets:update', 'podman-secrets:read'),
    ('podman-secrets:delete', 'podman-secrets:list'),
    ('podman-secrets:delete', 'podman-secrets:read'),
    ('podman-kube:read', 'podman:read'),
    ('podman-kube:create', 'podman:read'),
    ('podman-kube:delete', 'podman:read'),
    ('gitops:read', 'gitops:list'),
    ('gitops:create', 'gitops:list'),
    ('gitops:create', 'gitops:read'),
    ('gitops:update', 'gitops:list'),
    ('gitops:update', 'gitops:read'),
    ('gitops:delete', 'gitops:list'),
    ('gitops:delete', 'gitops:read'),
    ('gitops:sync', 'gitops:list'),
    ('gitops:sync', 'gitops:read'),
    ('gitops:sync', 'activities:list'),
    ('gitops:sync', 'activities:read'),
    ('gitops:lifecycle', 'gitops:list'),
    ('gitops:lifecycle', 'gitops:read'),
    ('gitops:backup', 'gitops:list'),
    ('gitops:backup', 'gitops:read'),
    ('webhooks:create', 'webhooks:list'),
    ('webhooks:update', 'webhooks:list'),
    ('webhooks:delete', 'webhooks:list'),
    ('jobs:read', 'jobs:list'),
    ('jobs:update', 'jobs:list'),
    ('jobs:update', 'jobs:read'),
    ('jobs:run', 'jobs:list'),
    ('jobs:run', 'jobs:read'),
    ('jobs:run', 'activities:list'),
    ('jobs:run', 'activities:read'),
    ('jobs:restart', 'jobs:list'),
    ('jobs:restart', 'jobs:read'),
    ('jobs:retry', 'jobs:list'),
    ('jobs:retry', 'jobs:read'),
    ('jobs:retry', 'activities:list'),
    ('jobs:retry', 'activities:read'),
    ('jobs:cancel', 'jobs:list'),
    ('jobs:cancel', 'jobs:read'),
    ('jobs:ack', 'jobs:list'),
    ('jobs:ack', 'jobs:read'),
    ('notifications:read', 'notifications:list'),
    ('notifications:update', 'notifications:list'),
    ('notifications:update', 'notifications:read'),
    ('notifications:delete', 'notifications:list'),
    ('notifications:delete', 'notifications:read'),
    ('notifications:test', 'notifications:list'),
    ('notifications:test', 'notifications:read'),
    ('system:prune', 'system:read'),
    ('system:prune', 'activities:list'),
    ('system:prune', 'activities:read'),
    ('system:upgrade', 'system:read'),
    ('image-updates:read', 'image-updates:list'),
    ('image-updates:check', 'image-updates:list'),
    ('image-updates:check', 'image-updates:read'),
    ('image-updates:check', 'activities:list'),
    ('image-updates:check', 'activities:read'),
    ('image-updates:apply', 'image-updates:list'),
    ('image-updates:apply', 'image-updates:read'),
    ('image-updates:apply', 'activities:list'),
    ('image-updates:apply', 'activities:read'),
    ('vulnerabilities:read', 'vulnerabilities:list'),
    ('vulnerabilities:scan', 'vulnerabilities:list'),
    ('vulnerabilities:scan', 'vulnerabilities:read'),
    ('vulnerabilities:scan', 'activities:list'),
    ('vulnerabilities:scan', 'activities:read'),
    ('vulnerabilities:update', 'vulnerabilities:list'),
    ('vulnerabilities:update', 'vulnerabilities:read'),
    ('build-workspaces:read', 'build-workspaces:list'),
    ('build-workspaces:create', 'build-workspaces:list'),
    ('build-workspaces:create', 'build-workspaces:read'),
    ('build-workspaces:delete', 'build-workspaces:list'),
    ('build-workspaces:delete', 'build-workspaces:read'),
    ('activities:read', 'activities:list'),
    ('activities:cancel', 'activities:list'),
    ('activities:cancel', 'activities:read'),
    ('activities:delete', 'activities:list'),
    ('activities:delete', 'activities:read'),
    ('environment-settings:update', 'environment-settings:read'),
    ('swarm-services:list', 'swarm:read'),
    ('swarm-services:read', 'swarm:read'),
    ('swarm-services:create', 'swarm:read'),
    ('swarm-services:update', 'swarm:read'),
    ('swarm-services:delete', 'swarm:read'),
    ('swarm-services:logs', 'swarm:read'),
    ('swarm-stacks:list', 'swarm:read'),
    ('swarm-stacks:read', 'swarm:read'),
    ('swarm-stacks:create', 'swarm:read'),
    ('swarm-stacks:update', 'swarm:read'),
    ('swarm-stacks:delete', 'swarm:read'),
    ('swarm-nodes:list', 'swarm:read'),
    ('swarm-nodes:read', 'swarm:read'),
    ('swarm-nodes:update', 'swarm:read'),
    ('swarm-nodes:delete', 'swarm:read'),
    ('swarm-nodes:promote', 'swarm:read'),
    ('swarm-nodes:demote', 'swarm:read'),
    ('swarm-configs:list', 'swarm:read'),
    ('swarm-configs:read', 'swarm:read'),
    ('swarm-configs:create', 'swarm:read'),
    ('swarm-configs:update', 'swarm:read'),
    ('swarm-configs:delete', 'swarm:read'),
    ('swarm-secrets:list', 'swarm:read'),
    ('swarm-secrets:read', 'swarm:read'),
    ('swarm-secrets:create', 'swarm:read'),
    ('swarm-secrets:update', 'swarm:read'),
    ('swarm-secrets:delete', 'swarm:read'),
    ('podman-pods:list', 'podman:read'),
    ('podman-pods:read', 'podman:read'),
    ('podman-pods:create', 'podman:read'),
    ('podman-pods:delete', 'podman:read'),
    ('podman-pods:prune', 'podman:read'),
    ('podman-pods:start', 'podman:read'),
    ('podman-pods:stop', 'podman:read'),
    ('podman-pods:restart', 'podman:read'),
    ('podman-pods:pause', 'podman:read'),
    ('podman-pods:kill', 'podman:read'),
    ('podman-quadlets:list', 'podman:read'),
    ('podman-quadlets:read', 'podman:read'),
    ('podman-quadlets:create', 'podman:read'),
    ('podman-quadlets:update', 'podman:read'),
    ('podman-quadlets:delete', 'podman:read'),
    ('podman-quadlets:start', 'podman:read'),
    ('podman-quadlets:stop', 'podman:read'),
    ('podman-quadlets:restart', 'podman:read'),
    ('podman-secrets:list', 'podman:read'),
    ('podman-secrets:read', 'podman:read'),
    ('podman-secrets:create', 'podman:read'),
    ('podman-secrets:update', 'podman:read'),
    ('podman-secrets:delete', 'podman:read');
`
	v102SQLiteGrantFullCatalog = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(permission)
    FROM (
        SELECT value AS permission FROM json_each(roles.permissions)
        UNION
        SELECT 'diagnostics:profile'
        UNION
        SELECT 'environments:restart'
    )
)
WHERE NOT roles.built_in
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS catalog
        WHERE catalog.old_permission NOT IN ('diagnostics:profile', 'environments:restart')
            AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = catalog.old_permission)
    );
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    scope.api_key_id,
    added.permission,
    NULL,
    CURRENT_TIMESTAMP
FROM (SELECT DISTINCT api_key_id FROM api_key_permissions WHERE environment_id IS NULL) AS scope
CROSS JOIN (SELECT 'diagnostics:profile' AS permission UNION ALL SELECT 'environments:restart') AS added
WHERE NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS held
        WHERE held.api_key_id = scope.api_key_id
            AND held.permission = added.permission
            AND held.environment_id IS NULL
    )
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS catalog
        WHERE catalog.old_permission NOT IN ('diagnostics:profile', 'environments:restart')
            AND NOT EXISTS (
                SELECT 1 FROM api_key_permissions AS held
                WHERE held.api_key_id = scope.api_key_id
                    AND held.permission = catalog.old_permission
                    AND held.environment_id IS NULL
            )
    );
`
	v102PostgresGrantFullCatalog = `
UPDATE roles
SET permissions = (
    SELECT jsonb_agg(DISTINCT permission)
    FROM jsonb_array_elements_text(roles.permissions || '["diagnostics:profile","environments:restart"]'::jsonb) AS permission
)
WHERE NOT roles.built_in
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS catalog
        WHERE catalog.old_permission NOT IN ('diagnostics:profile', 'environments:restart')
            AND NOT roles.permissions @> jsonb_build_array(catalog.old_permission)
    );
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    gen_random_uuid()::text,
    scope.api_key_id,
    added.permission,
    NULL,
    CURRENT_TIMESTAMP
FROM (SELECT DISTINCT api_key_id FROM api_key_permissions WHERE environment_id IS NULL) AS scope
CROSS JOIN (VALUES ('diagnostics:profile'), ('environments:restart')) AS added(permission)
WHERE NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS held
        WHERE held.api_key_id = scope.api_key_id
            AND held.permission = added.permission
            AND held.environment_id IS NULL
    )
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS catalog
        WHERE catalog.old_permission NOT IN ('diagnostics:profile', 'environments:restart')
            AND NOT EXISTS (
                SELECT 1 FROM api_key_permissions AS held
                WHERE held.api_key_id = scope.api_key_id
                    AND held.permission = catalog.old_permission
                    AND held.environment_id IS NULL
            )
    );
`
	v102RevertMap = v102RenameMap + `
DELETE FROM rbac_permission_map WHERE old_permission = 'settings:update';
`
	v102SQLiteRevertApply = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(reverted.permission)
    FROM (
        SELECT granted.value AS permission
        FROM json_each(roles.permissions) AS granted
        WHERE granted.value NOT IN ('diagnostics:profile', 'environments:restart')
            AND granted.value NOT IN (SELECT new_permission FROM rbac_permission_map)
        UNION
        SELECT split.old_permission
        FROM (SELECT DISTINCT old_permission FROM rbac_permission_map) AS split
        WHERE NOT EXISTS (
            SELECT 1 FROM rbac_permission_map AS part
            WHERE part.old_permission = split.old_permission
                AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) AS held WHERE held.value = part.new_permission)
        )
    ) AS reverted
)
WHERE EXISTS (
    SELECT 1 FROM json_each(roles.permissions) AS granted
    WHERE granted.value IN ('diagnostics:profile', 'environments:restart')
        OR granted.value IN (SELECT new_permission FROM rbac_permission_map)
);
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    scope.api_key_id,
    split.old_permission,
    scope.environment_id,
    CURRENT_TIMESTAMP
FROM (SELECT DISTINCT api_key_id, environment_id FROM api_key_permissions) AS scope
CROSS JOIN (SELECT DISTINCT old_permission FROM rbac_permission_map) AS split
WHERE NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = scope.api_key_id
            AND existing.permission = split.old_permission
            AND COALESCE(existing.environment_id, '') = COALESCE(scope.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS part
        WHERE part.old_permission = split.old_permission
            AND NOT EXISTS (
                SELECT 1 FROM api_key_permissions AS held
                WHERE held.api_key_id = scope.api_key_id
                    AND held.permission = part.new_permission
                    AND COALESCE(held.environment_id, '') = COALESCE(scope.environment_id, '')
            )
    );
DELETE FROM api_key_permissions
WHERE (permission IN ('diagnostics:profile', 'environments:restart') OR permission IN (SELECT new_permission FROM rbac_permission_map))
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS split
        WHERE split.old_permission = api_key_permissions.permission
            AND NOT EXISTS (
                SELECT 1 FROM rbac_permission_map AS part
                WHERE part.old_permission = split.old_permission
                    AND NOT EXISTS (
                        SELECT 1 FROM api_key_permissions AS held
                        WHERE held.api_key_id = api_key_permissions.api_key_id
                            AND held.permission = part.new_permission
                            AND COALESCE(held.environment_id, '') = COALESCE(api_key_permissions.environment_id, '')
                    )
            )
    );
`
	v102PostgresRevertApply = `
UPDATE roles
SET permissions = COALESCE((
    SELECT jsonb_agg(reverted.permission ORDER BY reverted.permission)
    FROM (
        SELECT granted.value AS permission
        FROM jsonb_array_elements_text(roles.permissions) AS granted(value)
        WHERE granted.value NOT IN ('diagnostics:profile', 'environments:restart')
            AND granted.value NOT IN (SELECT new_permission FROM rbac_permission_map)
        UNION
        SELECT split.old_permission
        FROM (SELECT DISTINCT old_permission FROM rbac_permission_map) AS split
        WHERE NOT EXISTS (
            SELECT 1 FROM rbac_permission_map AS part
            WHERE part.old_permission = split.old_permission
                AND NOT roles.permissions @> jsonb_build_array(part.new_permission)
        )
    ) AS reverted
), '[]'::jsonb)
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements_text(roles.permissions) AS granted(value)
    WHERE granted.value IN ('diagnostics:profile', 'environments:restart')
        OR granted.value IN (SELECT new_permission FROM rbac_permission_map)
);
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    gen_random_uuid()::text,
    scope.api_key_id,
    split.old_permission,
    scope.environment_id,
    CURRENT_TIMESTAMP
FROM (SELECT DISTINCT api_key_id, environment_id FROM api_key_permissions) AS scope
CROSS JOIN (SELECT DISTINCT old_permission FROM rbac_permission_map) AS split
WHERE NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = scope.api_key_id
            AND existing.permission = split.old_permission
            AND COALESCE(existing.environment_id, '') = COALESCE(scope.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS part
        WHERE part.old_permission = split.old_permission
            AND NOT EXISTS (
                SELECT 1 FROM api_key_permissions AS held
                WHERE held.api_key_id = scope.api_key_id
                    AND held.permission = part.new_permission
                    AND COALESCE(held.environment_id, '') = COALESCE(scope.environment_id, '')
            )
    );
DELETE FROM api_key_permissions
WHERE (permission IN ('diagnostics:profile', 'environments:restart') OR permission IN (SELECT new_permission FROM rbac_permission_map))
    AND NOT EXISTS (
        SELECT 1 FROM rbac_permission_map AS split
        WHERE split.old_permission = api_key_permissions.permission
            AND NOT EXISTS (
                SELECT 1 FROM rbac_permission_map AS part
                WHERE part.old_permission = split.old_permission
                    AND NOT EXISTS (
                        SELECT 1 FROM api_key_permissions AS held
                        WHERE held.api_key_id = api_key_permissions.api_key_id
                            AND held.permission = part.new_permission
                            AND COALESCE(held.environment_id, '') = COALESCE(api_key_permissions.environment_id, '')
                    )
            )
    );
`
	v102SQLiteApplyMap = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(DISTINCT COALESCE(mapping.new_permission, granted.value))
    FROM json_each(roles.permissions) AS granted
    LEFT JOIN rbac_permission_map AS mapping ON mapping.old_permission = granted.value
)
WHERE EXISTS (
    SELECT 1 FROM json_each(roles.permissions) AS granted
    JOIN rbac_permission_map AS mapping ON mapping.old_permission = granted.value
);
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    mapped.api_key_id,
    mapped.permission,
    mapped.environment_id,
    CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT source.api_key_id, mapping.new_permission AS permission, source.environment_id
    FROM api_key_permissions AS source
    JOIN rbac_permission_map AS mapping ON mapping.old_permission = source.permission
    WHERE mapping.new_permission <> mapping.old_permission
) AS mapped
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = mapped.api_key_id
        AND existing.permission = mapped.permission
        AND COALESCE(existing.environment_id, '') = COALESCE(mapped.environment_id, '')
);
DELETE FROM api_key_permissions
WHERE permission IN (SELECT old_permission FROM rbac_permission_map)
    AND permission NOT IN (SELECT new_permission FROM rbac_permission_map);
`
	v102PostgresApplyMap = `
UPDATE roles
SET permissions = (
    SELECT jsonb_agg(expanded.permission ORDER BY expanded.element_position, expanded.ordinal)
    FROM (
        SELECT COALESCE(mapping.new_permission, granted.value) AS permission,
            MIN(granted.element_position) AS element_position,
            MIN(COALESCE(mapping.ordinal, 0)) AS ordinal
        FROM jsonb_array_elements_text(roles.permissions) WITH ORDINALITY AS granted(value, element_position)
        LEFT JOIN rbac_permission_map AS mapping ON mapping.old_permission = granted.value
        GROUP BY 1
    ) AS expanded
)
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements_text(roles.permissions) AS granted(value)
    JOIN rbac_permission_map AS mapping ON mapping.old_permission = granted.value
);
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, mapped.api_key_id, mapped.permission, mapped.environment_id, CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT source.api_key_id, mapping.new_permission AS permission, source.environment_id
    FROM api_key_permissions AS source
    JOIN rbac_permission_map AS mapping ON mapping.old_permission = source.permission
    WHERE mapping.new_permission <> mapping.old_permission
) AS mapped
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = mapped.api_key_id
        AND existing.permission = mapped.permission
        AND COALESCE(existing.environment_id, '') = COALESCE(mapped.environment_id, '')
);
DELETE FROM api_key_permissions
WHERE permission IN (SELECT old_permission FROM rbac_permission_map)
    AND permission NOT IN (SELECT new_permission FROM rbac_permission_map);
`
	v102DropMap = `
DROP TABLE rbac_permission_map;
`
	v102SQLiteUp = v102CleanupSQLiteUp + v102SQLiteCreateMap + v102RenameMap + v102SQLiteApplyMap +
		v102RequiresMap + v102SQLiteApplyMap + v102SQLiteGrantFullCatalog + v102DropMap
	v102PostgresUp = v102CleanupPostgresUp + v102PostgresCreateMap + v102RenameMap + v102PostgresApplyMap +
		v102RequiresMap + v102PostgresApplyMap + v102PostgresGrantFullCatalog + v102DropMap
	v102SQLiteDown   = v102SQLiteCreateMap + v102RevertMap + v102SQLiteRevertApply + v102DropMap + v102CleanupSQLiteDown
	v102PostgresDown = v102PostgresCreateMap + v102RevertMap + v102PostgresRevertApply + v102DropMap + v102CleanupPostgresDown
)

func init() {
	goose.AddMigrationContext(run(v102SQLiteUp, v102PostgresUp), run(v102SQLiteDown, v102PostgresDown))
}
