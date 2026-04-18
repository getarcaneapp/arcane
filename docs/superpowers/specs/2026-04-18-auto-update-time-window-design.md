# Auto-Update Time Window — Design Spec

**Issue**: getarcaneapp/arcane#491
**Date**: 2026-04-18
**Author**: GiulioSavini

## Problem

Auto-update currently fires on a cron schedule (`autoUpdateInterval`, default: midnight daily). Users cannot restrict updates to a safe time window (e.g. 02:00–04:00), which causes unwanted service disruption during active hours. Watchtower supports this; its absence blocks migration.

## Goal

Allow users to define a time window + optional day-of-week restriction during which auto-updates are permitted. Updates detected outside the window accumulate as pending and are applied the next time the window opens.

## Non-Goals

- Per-project or per-container window overrides (global setting only)
- "Deadline" enforcement (stopping an in-progress update at window end)
- Timezone per-environment (uses existing server timezone setting)

---

## Architecture

### Core Mechanism (Option B)

When the window is **enabled**:
- `auto_update_job.Schedule()` returns `*/5 * * * * *` (check every 5 min)
- `auto_update_job.Run()` reads window settings, computes whether `now` falls inside the window+days; skips `ApplyPending` if not
- `ApplyPending` is already idempotent — running it multiple times during a window is safe

When the window is **disabled**:
- Behavior is unchanged: job runs on `autoUpdateInterval` cron

The 5-minute poll granularity means an update detected at 02:30 is applied by 02:35 at the latest.

### Overnight Windows

If `windowStart > windowEnd` (e.g. 23:00–01:00), the check wraps midnight correctly:
`now >= start OR now < end`

---

## Backend Changes

### 1. New Settings (4 keys)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `autoUpdateWindowEnabled` | bool | `false` | Enable window restriction |
| `autoUpdateWindowStart` | string HH:MM | `"02:00"` | Window open time |
| `autoUpdateWindowEnd` | string HH:MM | `"04:00"` | Window close time |
| `autoUpdateWindowDays` | string CSV | `"0,1,2,3,4,5,6"` | Days active (0=Sun…6=Sat) |

### 2. Files to modify

**`backend/internal/models/settings.go`**
Add 4 `SettingVariable` entries with appropriate `meta` tags (label, type, category=internal).

**`backend/internal/configschema/schema.go`**
Register the 4 new keys with type, default, and validation rules.

**`backend/pkg/libarcane/settings.go`**
Add the 4 keys to the allowed-settings allowlist.

**`backend/pkg/scheduler/auto_update_job.go`**
- `Schedule(ctx)`: if `autoUpdateWindowEnabled` is true, return `"*/5 * * * * *"`; otherwise fall through to existing `autoUpdateInterval` logic
- `Run(ctx)`: after the existing enable/polling checks, if window enabled, call new `isWithinWindow(ctx)` helper; return early if not inside window
- `isWithinWindow(ctx) bool`: parses `autoUpdateWindowStart`, `autoUpdateWindowEnd`, `autoUpdateWindowDays`; compares against `time.Now()` in server timezone; handles overnight ranges

**`backend/internal/services/settings_service.go`**
Add `autoUpdateWindowEnabled` to the existing reschedule trigger so changing the window setting reschedules the job immediately (same pattern as `autoUpdate` and `pollingEnabled`).

**`backend/internal/configschema/schema_test.go`**
Add the 4 new keys to the known-keys list.

**`backend/pkg/scheduler/auto_update_job_test.go`**
Add tests for:
- `isWithinWindow` with normal range, overnight range, day filter
- `Schedule()` returns `*/5 * * * * *` when window enabled
- `Run()` skips when outside window

---

## Frontend Changes

### Location

Jobs tab → `auto-update` card → below existing "excluded containers" section.

### New UI Block

```
┌─────────────────────────────────────────┐
│  [x] Restrict updates to a time window  │
│                                         │
│  From  [02:00]  →  To  [04:00]          │
│                                         │
│  Days:  M  T  W  T  F  S  S            │
│        [■][■][■][■][■][■][■]            │
│                                         │
│  ▶ Advanced — cron expression           │  ← <details> collapsed
│    [0 */5 * * * *]  (read-only)         │
└─────────────────────────────────────────┘
```

When window toggle is **off**: advanced cron is editable (existing behavior via `JobScheduleDialog`), time/days pickers are hidden.

When window toggle is **on**: time+days pickers visible, cron field is read-only and shows the `*/5 * * * * *` value with a tooltip explaining it.

### Files to modify

**`frontend/src/lib/types/settings.type.ts`**
Add `autoUpdateWindowEnabled`, `autoUpdateWindowStart`, `autoUpdateWindowEnd`, `autoUpdateWindowDays` fields to `Settings`.

**`frontend/src/routes/(app)/environments/[id]/+page.svelte`**
- Add 4 new fields to the zod schema
- Add default values in settings → form mapping
- Include in save payload

**`frontend/src/routes/(app)/environments/[id]/components/JobsTab.svelte`**
- Add window toggle (`Switch`)
- Add time range pickers (`<input type="time">`) — native, cross-browser
- Add day-of-week badge toggles (7 clickable badges, pattern used elsewhere in arcane)
- Add `<details>` for advanced cron (read-only when window active)

**`frontend/src/lib/paraglide/messages/`** (i18n)
New keys:
- `auto_update_window_enabled`
- `auto_update_window_from`
- `auto_update_window_to`
- `auto_update_window_days`
- `auto_update_window_cron_readonly_hint`

---

## Data Flow

```
User sets window 02:00–04:00, Mon–Fri
  → PATCH /environments/:id/settings
  → settingsService saves 4 keys
  → reschedule hook fires → scheduler updates auto-update job to */5 cron
  → at 02:00 job wakes, isWithinWindow=true, ApplyPending runs
  → at 02:05 job wakes, isWithinWindow=true, ApplyPending runs (idempotent if no new updates)
  → at 04:00 job wakes, isWithinWindow=false, skips
```

---

## Testing Plan

- Unit: `isWithinWindow` covers normal, overnight, day-filter, edge (exactly on boundary)
- Unit: `Schedule()` returns correct cron in both modes
- Unit: `Run()` skips outside window, runs inside window
- Manual: enable window 02:00–04:00, set clock to 03:00 → update applied; set to 05:00 → skipped
- Manual: overnight window 23:00–01:00 wraps correctly

---

## Out of Scope for This PR

- i18n for languages other than English (crowdin will pick up new keys)
- Per-container window overrides
- Notification "skipped due to window" (could be a follow-up)
