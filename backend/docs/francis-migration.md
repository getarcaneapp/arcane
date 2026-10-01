# Durable jobs migration

Stop the old Arcane process before upgrading to the Francis-based release. Take a system backup before the upgrade. The first startup imports legacy persisted job records before accepting requests or executing jobs. Import resumes safely if startup is interrupted. Legacy records remain in the database but are never dispatched by the new runtime.

Only one active Arcane process may use a database. Francis enforces this when the actor host registers. After an unclean shutdown, startup can wait for the previous registration to expire. Do not run the old and new releases against the same database at the same time.

`ACTOR_PORT` defaults to `3551`. Francis uses QUIC over UDP bound to `127.0.0.1`. Do not publish this port from Docker. Processes sharing a network namespace must use different actor ports, even when they use different databases. No separate actor authentication secret is needed. Arcane derives it from the existing encryption key and instance ID.

To roll back, stop Arcane and restore the pre-upgrade backup before starting the old release. Do not point the old release at the upgraded database. Restore accepts existing database/WAL/SHM archives and the new staged single-file database snapshot. The stopped restore helper removes copied live host ownership before restarting Arcane. Normal startup repairs schedules and pending dispatches from durable state.
