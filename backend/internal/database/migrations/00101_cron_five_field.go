package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v101SQLiteUp = `
UPDATE settings SET value = substr(value, instr(value, ' ') + 1)
WHERE key IN (
    'environmentHealthInterval', 'autoUpdateInterval', 'pollingInterval', 'dockerClientRefreshInterval', 'eventCleanupInterval',
    'expiredSessionsCleanupInterval', 'scheduledPruneInterval', 'autoHealInterval', 'vulnerabilityScanInterval', 'imageAutoPatchInterval'
)
    AND value NOT LIKE '@%' AND length(value) - length(replace(value, ' ', '')) = 5;
UPDATE system_backup_policies SET schedule = substr(schedule, instr(schedule, ' ') + 1)
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 5;
UPDATE volume_backup_policies SET schedule = substr(schedule, instr(schedule, ' ') + 1)
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 5;
UPDATE settings SET value = (
    SELECT json_set(settings.value, '$.policies', json_group_array(json(json_replace(p.value, '$.schedule',
        CASE WHEN json_extract(p.value, '$.schedule') NOT LIKE '@%'
            AND length(json_extract(p.value, '$.schedule')) - length(replace(json_extract(p.value, '$.schedule'), ' ', '')) = 5
        THEN substr(json_extract(p.value, '$.schedule'), instr(json_extract(p.value, '$.schedule'), ' ') + 1)
        ELSE json_extract(p.value, '$.schedule') END))))
    FROM json_each(settings.value, '$.policies') AS p
)
WHERE key = 'systemVolumeBackupConfig' AND json_valid(value) AND json_type(value, '$.policies') = 'array';
`
	v101PostgresUp = `
UPDATE settings SET value = substr(value, strpos(value, ' ') + 1)
WHERE key IN (
    'environmentHealthInterval', 'autoUpdateInterval', 'pollingInterval', 'dockerClientRefreshInterval', 'eventCleanupInterval',
    'expiredSessionsCleanupInterval', 'scheduledPruneInterval', 'autoHealInterval', 'vulnerabilityScanInterval', 'imageAutoPatchInterval'
)
    AND value NOT LIKE '@%' AND length(value) - length(replace(value, ' ', '')) = 5;
UPDATE system_backup_policies SET schedule = substr(schedule, strpos(schedule, ' ') + 1)
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 5;
UPDATE volume_backup_policies SET schedule = substr(schedule, strpos(schedule, ' ') + 1)
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 5;
UPDATE settings SET value = jsonb_set(value::jsonb, '{policies}', (
    SELECT coalesce(jsonb_agg(CASE
        WHEN p ->> 'schedule' NOT LIKE '@%' AND length(p ->> 'schedule') - length(replace(p ->> 'schedule', ' ', '')) = 5
        THEN jsonb_set(p, '{schedule}', to_jsonb(substr(p ->> 'schedule', strpos(p ->> 'schedule', ' ') + 1)))
        ELSE p END ORDER BY ord), '[]'::jsonb)
    FROM jsonb_array_elements(value::jsonb -> 'policies') WITH ORDINALITY AS t(p, ord)
))::text
WHERE key = 'systemVolumeBackupConfig' AND jsonb_typeof(value::jsonb -> 'policies') = 'array';
`
	v101Down = `
UPDATE settings SET value = '0 ' || value
WHERE key IN (
    'environmentHealthInterval', 'autoUpdateInterval', 'pollingInterval', 'dockerClientRefreshInterval', 'eventCleanupInterval',
    'expiredSessionsCleanupInterval', 'scheduledPruneInterval', 'autoHealInterval', 'vulnerabilityScanInterval', 'imageAutoPatchInterval'
)
    AND value NOT LIKE '@%' AND length(value) - length(replace(value, ' ', '')) = 4;
UPDATE system_backup_policies SET schedule = '0 ' || schedule
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 4;
UPDATE volume_backup_policies SET schedule = '0 ' || schedule
WHERE schedule NOT LIKE '@%' AND length(schedule) - length(replace(schedule, ' ', '')) = 4;
`
	v101SQLiteDown = v101Down + `
UPDATE settings SET value = (
    SELECT json_set(settings.value, '$.policies', json_group_array(json(json_replace(p.value, '$.schedule',
        CASE WHEN json_extract(p.value, '$.schedule') NOT LIKE '@%'
            AND length(json_extract(p.value, '$.schedule')) - length(replace(json_extract(p.value, '$.schedule'), ' ', '')) = 4
        THEN '0 ' || json_extract(p.value, '$.schedule')
        ELSE json_extract(p.value, '$.schedule') END))))
    FROM json_each(settings.value, '$.policies') AS p
)
WHERE key = 'systemVolumeBackupConfig' AND json_valid(value) AND json_type(value, '$.policies') = 'array';
`
	v101PostgresDown = v101Down + `
UPDATE settings SET value = jsonb_set(value::jsonb, '{policies}', (
    SELECT coalesce(jsonb_agg(CASE
        WHEN p ->> 'schedule' NOT LIKE '@%' AND length(p ->> 'schedule') - length(replace(p ->> 'schedule', ' ', '')) = 4
        THEN jsonb_set(p, '{schedule}', to_jsonb('0 ' || (p ->> 'schedule')))
        ELSE p END ORDER BY ord), '[]'::jsonb)
    FROM jsonb_array_elements(value::jsonb -> 'policies') WITH ORDINALITY AS t(p, ord)
))::text
WHERE key = 'systemVolumeBackupConfig' AND jsonb_typeof(value::jsonb -> 'policies') = 'array';
`
)

func init() {
	goose.AddMigrationContext(run(v101SQLiteUp, v101PostgresUp), run(v101SQLiteDown, v101PostgresDown))
}
