package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v123Up = `
INSERT INTO backup_policies (
    id, subject_kind, subject_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, options, created_at, updated_at
)
SELECT id, 'system', '', enabled, schedule, retention_count, false, local_enabled, s3_enabled,
    COALESCE(s3_destination_id, ''), '{}', created_at, updated_at
FROM system_backup_policies;
DROP TABLE system_backup_policies;
`
	v123SQLiteUp = v123Up + `
INSERT INTO backup_policies (
    id, subject_kind, subject_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, options, created_at
)
SELECT json_extract(p.value, '$.id'), 'volume', '',
    COALESCE(json_extract(p.value, '$.enabled'), 0),
    COALESCE(json_extract(p.value, '$.schedule'), ''),
    COALESCE(json_extract(p.value, '$.retentionCount'), 0),
    COALESCE(json_extract(p.value, '$.stopContainers'), 0),
    COALESCE(json_extract(p.value, '$.localEnabled'), 0),
    COALESCE(json_extract(p.value, '$.s3Enabled'), 0),
    COALESCE(json_extract(p.value, '$.s3DestinationId'), ''),
    json_object(
        'selectionMode', COALESCE(json_extract(p.value, '$.selectionMode'), ''),
        'volumeNames', json(COALESCE(json_extract(p.value, '$.volumeNames'), 'null')),
        'ignoreAnonymous', json(CASE WHEN json_extract(p.value, '$.ignoreAnonymous') THEN 'true' ELSE 'false' END)
    ),
    datetime('now', '+' || p.key || ' seconds')
FROM settings, json_each(settings.value, '$.policies') AS p
WHERE settings.key = 'systemVolumeBackupConfig' AND json_valid(settings.value)
    AND json_type(settings.value, '$.policies') = 'array' AND COALESCE(json_extract(p.value, '$.id'), '') <> '';
DELETE FROM settings WHERE key = 'systemVolumeBackupConfig';
`
	v123PostgresUp = v123Up + `
INSERT INTO backup_policies (
    id, subject_kind, subject_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, options, created_at
)
SELECT p ->> 'id', 'volume', '',
    COALESCE((p ->> 'enabled')::boolean, false),
    COALESCE(p ->> 'schedule', ''),
    COALESCE((p ->> 'retentionCount')::integer, 0),
    COALESCE((p ->> 'stopContainers')::boolean, false),
    COALESCE((p ->> 'localEnabled')::boolean, false),
    COALESCE((p ->> 's3Enabled')::boolean, false),
    COALESCE(p ->> 's3DestinationId', ''),
    jsonb_build_object(
        'selectionMode', COALESCE(p ->> 'selectionMode', ''),
        'volumeNames', COALESCE(p -> 'volumeNames', 'null'::jsonb),
        'ignoreAnonymous', COALESCE((p ->> 'ignoreAnonymous')::boolean, false)
    )::text,
    CURRENT_TIMESTAMP + ord * interval '1 second'
FROM settings, jsonb_array_elements(settings.value::jsonb -> 'policies') WITH ORDINALITY AS t(p, ord)
WHERE settings.key = 'systemVolumeBackupConfig' AND jsonb_typeof(settings.value::jsonb -> 'policies') = 'array'
    AND COALESCE(p ->> 'id', '') <> '';
DELETE FROM settings WHERE key = 'systemVolumeBackupConfig';
`
	v123SQLiteDown = `
CREATE TABLE system_backup_policies (
    id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies(s3_destination_id);
INSERT INTO system_backup_policies (
    id, enabled, schedule, retention_count, local_enabled, s3_enabled, s3_destination_id, created_at, updated_at
)
SELECT id, enabled, schedule, retention_count, local_enabled, s3_enabled, NULLIF(s3_destination_id, ''), created_at, updated_at
FROM backup_policies WHERE subject_kind = 'system';
DELETE FROM settings WHERE key = 'systemVolumeBackupConfig';
INSERT INTO settings (key, value, type, isInternal)
SELECT 'systemVolumeBackupConfig', json_object('policies', json_group_array(json_object(
    'id', id,
    'enabled', json(CASE WHEN enabled THEN 'true' ELSE 'false' END),
    'schedule', schedule,
    'retentionCount', retention_count,
    'stopContainers', json(CASE WHEN stop_containers THEN 'true' ELSE 'false' END),
    'localEnabled', json(CASE WHEN local_enabled THEN 'true' ELSE 'false' END),
    's3Enabled', json(CASE WHEN s3_enabled THEN 'true' ELSE 'false' END),
    's3DestinationId', s3_destination_id,
    'selectionMode', COALESCE(json_extract(options, '$.selectionMode'), ''),
    'volumeNames', json(COALESCE(json_extract(options, '$.volumeNames'), 'null')),
    'ignoreAnonymous', json(CASE WHEN json_extract(options, '$.ignoreAnonymous') THEN 'true' ELSE 'false' END)
))), 'string', true
FROM (SELECT * FROM backup_policies WHERE subject_kind = 'volume' AND subject_id = '' ORDER BY created_at, id);
DELETE FROM backup_policies WHERE subject_kind = 'system' OR (subject_kind = 'volume' AND subject_id = '');
`
	v123PostgresDown = `
CREATE TABLE system_backup_policies (
    id text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    schedule text NOT NULL,
    retention_count integer DEFAULT 7 NOT NULL,
    local_enabled boolean DEFAULT true NOT NULL,
    s3_enabled boolean DEFAULT false NOT NULL,
    s3_destination_id text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    CONSTRAINT system_backup_policies_pkey PRIMARY KEY (id)
);
CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies USING btree (s3_destination_id);
INSERT INTO system_backup_policies (
    id, enabled, schedule, retention_count, local_enabled, s3_enabled, s3_destination_id, created_at, updated_at
)
SELECT id, enabled, schedule, retention_count, local_enabled, s3_enabled, NULLIF(s3_destination_id, ''), created_at, updated_at
FROM backup_policies WHERE subject_kind = 'system';
DELETE FROM settings WHERE key = 'systemVolumeBackupConfig';
INSERT INTO settings (key, value, type, isinternal)
SELECT 'systemVolumeBackupConfig', jsonb_build_object('policies', COALESCE(jsonb_agg(jsonb_build_object(
    'id', id,
    'enabled', enabled,
    'schedule', schedule,
    'retentionCount', retention_count,
    'stopContainers', stop_containers,
    'localEnabled', local_enabled,
    's3Enabled', s3_enabled,
    's3DestinationId', s3_destination_id,
    'selectionMode', COALESCE(options::jsonb ->> 'selectionMode', ''),
    'volumeNames', COALESCE(options::jsonb -> 'volumeNames', 'null'::jsonb),
    'ignoreAnonymous', COALESCE((options::jsonb ->> 'ignoreAnonymous')::boolean, false)
) ORDER BY created_at, id), '[]'::jsonb))::text, 'string', true
FROM backup_policies WHERE subject_kind = 'volume' AND subject_id = '';
DELETE FROM backup_policies WHERE subject_kind = 'system' OR (subject_kind = 'volume' AND subject_id = '');
`
)

func init() {
	goose.AddMigrationContext(run(v123SQLiteUp, v123PostgresUp), run(v123SQLiteDown, v123PostgresDown))
}
