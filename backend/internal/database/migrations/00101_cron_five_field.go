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
)

func init() {
	goose.AddMigrationContext(run(v101SQLiteUp, v101PostgresUp), run(v101Down, v101Down))
}
