# Auto-Update Time Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users restrict automatic container updates to a configurable time window (e.g. 02:00–04:00, Mon–Fri) with a friendly UI in the Jobs tab.

**Architecture:** Four new settings keys (`autoUpdateWindowEnabled`, `autoUpdateWindowStart`, `autoUpdateWindowEnd`, `autoUpdateWindowDays`) are stored in the existing settings table. When the window is enabled, `auto_update_job.Schedule()` returns `*/5 * * * * *` so the job fires every 5 minutes; `Run()` gates `ApplyPending` on a time-window check. The frontend adds a toggle + time pickers + day badges inside the existing auto-update card in the Jobs tab, mirroring the excluded-containers pattern.

**Tech Stack:** Go 1.23 (backend), SvelteKit 5 + TypeScript (frontend), Paraglide i18n (`frontend/messages/en.json`), SQLite via GORM, testify for backend tests.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/internal/models/settings.go` | Modify | Add 4 new `SettingVariable` struct fields |
| `backend/pkg/libarcane/settings.go` | Modify | Add 4 keys to allowed-settings list |
| `backend/internal/configschema/schema.go` | Modify | Add schema entries with `requires` notes |
| `backend/internal/configschema/schema_test.go` | Modify | Register 4 new keys in known-keys list |
| `backend/internal/services/settings_service.go` | Modify | Add window keys to reschedule trigger |
| `backend/pkg/scheduler/auto_update_job.go` | Modify | `Schedule()` + `Run()` window logic + `isWithinWindow()` |
| `backend/pkg/scheduler/auto_update_job_test.go` | Modify | Tests for window logic |
| `frontend/messages/en.json` | Modify | Add 6 i18n strings |
| `frontend/src/lib/types/settings.type.ts` | Modify | Add 4 fields to `Settings` type |
| `frontend/src/routes/(app)/environments/[id]/+page.svelte` | Modify | Schema, defaults, save payload |
| `frontend/src/routes/(app)/environments/[id]/components/JobsTab.svelte` | Modify | Window UI block inside auto-update card |

---

## Task 1: Backend — new settings fields

**Files:**
- Modify: `backend/internal/models/settings.go`
- Modify: `backend/pkg/libarcane/settings.go`

- [ ] **Step 1: Add the 4 SettingVariable fields to the Settings struct**

Open `backend/internal/models/settings.go`. Find the block containing `AutoUpdate`, `AutoUpdateInterval`, `AutoUpdateExcludedContainers`. Add immediately after `AutoUpdateExcludedContainers`:

```go
AutoUpdateWindowEnabled SettingVariable `key:"autoUpdateWindowEnabled" meta:"label=Auto Update Window Enabled;type=boolean;keywords=auto,update,window,schedule,time,restrict;category=internal;description=Restrict automatic updates to a configured time window"`
AutoUpdateWindowStart   SettingVariable `key:"autoUpdateWindowStart" meta:"label=Auto Update Window Start;type=text;keywords=auto,update,window,start,time,schedule;category=internal;description=Start time of the auto-update window (HH:MM, 24h format)"`
AutoUpdateWindowEnd     SettingVariable `key:"autoUpdateWindowEnd" meta:"label=Auto Update Window End;type=text;keywords=auto,update,window,end,time,schedule;category=internal;description=End time of the auto-update window (HH:MM, 24h format)"`
AutoUpdateWindowDays    SettingVariable `key:"autoUpdateWindowDays" meta:"label=Auto Update Window Days;type=text;keywords=auto,update,window,days,week,schedule;category=internal;description=Comma-separated day numbers (0=Sun,1=Mon,...,6=Sat) when the window is active"`
```

- [ ] **Step 2: Register the 4 keys in the settings allowlist**

Open `backend/pkg/libarcane/settings.go`. Find the `var cronSettingKeys` block and the nearby settings allowlist. Add the 4 new keys to the allowlist (the string-slice that `GetStringSetting` / `SetBoolSetting` etc. check against):

```go
"autoUpdateWindowEnabled",
"autoUpdateWindowStart",
"autoUpdateWindowEnd",
"autoUpdateWindowDays",
```

- [ ] **Step 3: Verify the project compiles**

```bash
cd /home/ubuntu/arcane/backend && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /home/ubuntu/arcane
git add backend/internal/models/settings.go backend/pkg/libarcane/settings.go
git commit -m "feat: add auto-update window settings fields"
```

---

## Task 2: Backend — schema registration + reschedule trigger

**Files:**
- Modify: `backend/internal/configschema/schema.go`
- Modify: `backend/internal/configschema/schema_test.go`
- Modify: `backend/internal/services/settings_service.go`

- [ ] **Step 1: Add schema entries**

Open `backend/internal/configschema/schema.go`. Find the map literal that contains `"autoUpdateInterval": { requires: "..." }`. Add after it:

```go
"autoUpdateWindowEnabled": {
    requires: "AUTO_UPDATE=true and autoUpdateWindowEnabled=true to have effect at runtime.",
},
"autoUpdateWindowStart": {
    requires: "autoUpdateWindowEnabled=true to have effect at runtime.",
},
"autoUpdateWindowEnd": {
    requires: "autoUpdateWindowEnabled=true to have effect at runtime.",
},
"autoUpdateWindowDays": {
    requires: "autoUpdateWindowEnabled=true to have effect at runtime.",
},
```

- [ ] **Step 2: Register keys in schema test**

Open `backend/internal/configschema/schema_test.go`. Find the string slice that lists known setting keys (contains `"autoUpdate"`, `"autoUpdateExcludedContainers"`, `"autoUpdateInterval"`). Add the 4 new keys in alphabetical order:

```
"autoUpdateWindowDays",
"autoUpdateWindowEnabled",
"autoUpdateWindowEnd",
"autoUpdateWindowStart",
```

- [ ] **Step 3: Run schema test to verify**

```bash
cd /home/ubuntu/arcane/backend && go test ./internal/configschema/... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Add window keys to reschedule trigger**

Open `backend/internal/services/settings_service.go`. Find the `switch` block containing:

```go
case "autoUpdate", "autoUpdateInterval":
    changedAutoUpdate = true
```

Change it to:

```go
case "autoUpdate", "autoUpdateInterval",
    "autoUpdateWindowEnabled", "autoUpdateWindowStart",
    "autoUpdateWindowEnd", "autoUpdateWindowDays":
    changedAutoUpdate = true
```

- [ ] **Step 5: Build to verify**

```bash
cd /home/ubuntu/arcane/backend && go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
cd /home/ubuntu/arcane
git add backend/internal/configschema/schema.go backend/internal/configschema/schema_test.go backend/internal/services/settings_service.go
git commit -m "feat: register auto-update window settings in schema and reschedule trigger"
```

---

## Task 3: Backend — window logic in the scheduler (TDD)

**Files:**
- Modify: `backend/pkg/scheduler/auto_update_job.go`
- Modify: `backend/pkg/scheduler/auto_update_job_test.go`

- [ ] **Step 1: Write the failing tests**

Open `backend/pkg/scheduler/auto_update_job_test.go`. Append the following tests after the existing ones:

```go
func TestAutoUpdateJob_Schedule_WindowEnabled(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	// Default (window disabled): returns autoUpdateInterval cron
	require.Equal(t, "0 0 0 * * *", job.Schedule(ctx))

	// Enable window: must return every-5-min cron
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdateWindowEnabled", true))
	require.Equal(t, "*/5 * * * * *", job.Schedule(ctx))
}

func TestAutoUpdateJob_isWithinWindow_NormalRange(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	loc := time.UTC

	// Inside window
	require.True(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 3, 0, 0, 0, loc)))
	// Exactly on start boundary
	require.True(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 2, 0, 0, 0, loc)))
	// Outside window (before)
	require.False(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 1, 59, 0, 0, loc)))
	// Outside window (after)
	require.False(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 4, 0, 0, 0, loc)))
}

func TestAutoUpdateJob_isWithinWindow_OvernightRange(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "23:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "01:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	loc := time.UTC

	require.True(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 23, 30, 0, 0, loc)))
	require.True(t, job.isWithinWindow(ctx, time.Date(2026, 1, 2, 0, 30, 0, 0, loc)))
	require.False(t, job.isWithinWindow(ctx, time.Date(2026, 1, 1, 12, 0, 0, 0, loc)))
	require.False(t, job.isWithinWindow(ctx, time.Date(2026, 1, 2, 1, 0, 0, 0, loc)))
}

func TestAutoUpdateJob_isWithinWindow_DayFilter(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	// Weekdays only (Mon=1 … Fri=5)
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "1,2,3,4,5"))

	loc := time.UTC

	// 2026-01-05 is Monday (Weekday=1)
	monday := time.Date(2026, 1, 5, 3, 0, 0, 0, loc)
	require.True(t, job.isWithinWindow(ctx, monday))

	// 2026-01-04 is Sunday (Weekday=0)
	sunday := time.Date(2026, 1, 4, 3, 0, 0, 0, loc)
	require.False(t, job.isWithinWindow(ctx, sunday))
}

func TestAutoUpdateJob_Run_SkipsOutsideWindow(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", true))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", true))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdateWindowEnabled", true))
	// Window: 02:00–04:00 all days — but we'll pass a time clearly outside
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	// updaterService is nil — if Run tries to call ApplyPending it will panic.
	// The test passes only if Run returns early (outside window).
	loc := time.UTC
	outsideWindow := time.Date(2026, 1, 1, 10, 0, 0, 0, loc)
	require.NotPanics(t, func() {
		job.runAt(ctx, outsideWindow)
	})
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /home/ubuntu/arcane/backend && go test ./pkg/scheduler/... -run "TestAutoUpdateJob_Schedule_Window|TestAutoUpdateJob_isWithinWindow|TestAutoUpdateJob_Run_SkipsOutsideWindow" -v 2>&1 | tail -20
```

Expected: compilation error or test failures (methods don't exist yet).

- [ ] **Step 3: Implement the window logic**

Open `backend/pkg/scheduler/auto_update_job.go`.

**3a — Update imports** (add `"strconv"` if missing, add `"strings"` and `"time"` if missing):

```go
import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)
```

**3b — Update `Schedule()`**: replace the existing method body with:

```go
func (j *AutoUpdateJob) Schedule(ctx context.Context) string {
	if j.settingsService.GetBoolSetting(ctx, "autoUpdateWindowEnabled", false) {
		return "*/5 * * * * *"
	}

	s := j.settingsService.GetStringSetting(ctx, "autoUpdateInterval", "0 0 0 * * *")
	if s == "" {
		return "0 0 0 * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			i = 1440
		}
		if i%1440 == 0 {
			return fmt.Sprintf("0 0 0 */%d * *", i/1440)
		}
		if i%60 == 0 {
			return fmt.Sprintf("0 0 */%d * * *", i/60)
		}
		return fmt.Sprintf("0 */%d * * * *", i)
	}

	return s
}
```

**3c — Update `Run()`**: replace the existing method body with:

```go
func (j *AutoUpdateJob) Run(ctx context.Context) {
	j.runAt(ctx, time.Now())
}

func (j *AutoUpdateJob) runAt(ctx context.Context, now time.Time) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.DebugContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return
	}

	if j.settingsService.GetBoolSetting(ctx, "autoUpdateWindowEnabled", false) {
		if !j.isWithinWindow(ctx, now) {
			slog.DebugContext(ctx, "auto-update skipped: outside configured time window")
			return
		}
	}

	slog.InfoContext(ctx, "auto-update run started")

	result, err := j.updaterService.ApplyPending(ctx, false)
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		return
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
}
```

**3d — Add `isWithinWindow()`** (new method at end of file):

```go
// isWithinWindow reports whether now falls within the configured update window.
// It reads autoUpdateWindowStart (HH:MM), autoUpdateWindowEnd (HH:MM), and
// autoUpdateWindowDays (CSV of 0=Sun…6=Sat). An overnight range (start > end)
// wraps midnight correctly.
func (j *AutoUpdateJob) isWithinWindow(ctx context.Context, now time.Time) bool {
	startStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowStart", "02:00")
	endStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowEnd", "04:00")
	daysStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6")

	parseHHMM := func(s string) (h, m int, ok bool) {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			return 0, 0, false
		}
		var err error
		h, err = strconv.Atoi(parts[0])
		if err != nil || h < 0 || h > 23 {
			return 0, 0, false
		}
		m, err = strconv.Atoi(parts[1])
		if err != nil || m < 0 || m > 59 {
			return 0, 0, false
		}
		return h, m, true
	}

	startH, startM, ok1 := parseHHMM(startStr)
	endH, endM, ok2 := parseHHMM(endStr)
	if !ok1 || !ok2 {
		slog.WarnContext(ctx, "auto-update window: invalid time format; allowing update",
			"start", startStr, "end", endStr)
		return true
	}

	// Check day-of-week filter
	allowedDays := make(map[time.Weekday]bool)
	for part := range strings.SplitSeq(daysStr, ",") {
		part = strings.TrimSpace(part)
		if d, err := strconv.Atoi(part); err == nil && d >= 0 && d <= 6 {
			allowedDays[time.Weekday(d)] = true
		}
	}
	if len(allowedDays) > 0 && !allowedDays[now.Weekday()] {
		return false
	}

	// Convert now to minutes-since-midnight
	nowMins := now.Hour()*60 + now.Minute()
	startMins := startH*60 + startM
	endMins := endH*60 + endM

	if startMins < endMins {
		// Normal range: e.g. 02:00–04:00
		return nowMins >= startMins && nowMins < endMins
	}
	// Overnight range: e.g. 23:00–01:00
	return nowMins >= startMins || nowMins < endMins
}
```

- [ ] **Step 4: Run tests and confirm they pass**

```bash
cd /home/ubuntu/arcane/backend && go test ./pkg/scheduler/... -run "TestAutoUpdateJob" -v 2>&1 | tail -30
```

Expected: all `TestAutoUpdateJob_*` tests PASS.

- [ ] **Step 5: Run the full scheduler test suite**

```bash
cd /home/ubuntu/arcane/backend && go test ./pkg/scheduler/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/ubuntu/arcane
git add backend/pkg/scheduler/auto_update_job.go backend/pkg/scheduler/auto_update_job_test.go
git commit -m "feat: add time-window gating to auto-update scheduler"
```

---

## Task 4: Frontend — i18n strings

**Files:**
- Modify: `frontend/messages/en.json`

- [ ] **Step 1: Add the 6 new i18n keys**

Open `frontend/messages/en.json`. Find the block of `"auto_update_*"` keys and add after `"auto_update_excluded_containers"`:

```json
"auto_update_window_enabled": "Restrict updates to a time window",
"auto_update_window_from": "From",
"auto_update_window_to": "To",
"auto_update_window_days": "Active days",
"auto_update_window_cron_hint": "Checks every 5 minutes while the window is active",
"auto_update_window_cron_label": "Cron expression (advanced)"
```

- [ ] **Step 2: Regenerate paraglide messages**

```bash
cd /home/ubuntu/arcane/frontend && pnpm run build 2>&1 | grep -E "error|warn|paraglide" | head -20
```

Expected: no errors related to missing keys. (Full build may take time — just check for errors.)

Alternatively, just run the paraglide compile step if available:
```bash
cd /home/ubuntu/arcane/frontend && npx @inlang/paraglide-js compile --project ./project.inlang 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
cd /home/ubuntu/arcane
git add frontend/messages/en.json
git commit -m "feat: add i18n strings for auto-update time window"
```

---

## Task 5: Frontend — TypeScript types + form schema

**Files:**
- Modify: `frontend/src/lib/types/settings.type.ts`
- Modify: `frontend/src/routes/(app)/environments/[id]/+page.svelte`

- [ ] **Step 1: Extend the Settings type**

Open `frontend/src/lib/types/settings.type.ts`. Find the `Settings` export. Add after `autoUpdateExcludedContainers`:

```typescript
autoUpdateWindowEnabled?: boolean;
autoUpdateWindowStart?: string;
autoUpdateWindowEnd?: string;
autoUpdateWindowDays?: string;
```

- [ ] **Step 2: Add fields to the zod schema in the environment page**

Open `frontend/src/routes/(app)/environments/[id]/+page.svelte`. Find the zod schema block (the object passed to `z.object({...})`). After `autoUpdateExcludedContainers: z.string().optional()` add:

```typescript
autoUpdateWindowEnabled: z.boolean(),
autoUpdateWindowStart: z.string(),
autoUpdateWindowEnd: z.string(),
autoUpdateWindowDays: z.string(),
```

- [ ] **Step 3: Add default values**

In the same file, find the defaults block (where `autoUpdateExcludedContainers: settings?.autoUpdateExcludedContainers || ''` is). Add after it:

```typescript
autoUpdateWindowEnabled: settings?.autoUpdateWindowEnabled ?? false,
autoUpdateWindowStart: settings?.autoUpdateWindowStart ?? '02:00',
autoUpdateWindowEnd: settings?.autoUpdateWindowEnd ?? '04:00',
autoUpdateWindowDays: settings?.autoUpdateWindowDays ?? '0,1,2,3,4,5,6',
```

- [ ] **Step 4: Add to save payload**

In the same file, find the `updateSettingsForEnvironment` call. After `autoUpdateExcludedContainers: formData.autoUpdateExcludedContainers` add:

```typescript
autoUpdateWindowEnabled: formData.autoUpdateWindowEnabled,
autoUpdateWindowStart: formData.autoUpdateWindowStart,
autoUpdateWindowEnd: formData.autoUpdateWindowEnd,
autoUpdateWindowDays: formData.autoUpdateWindowDays,
```

- [ ] **Step 5: Type-check**

```bash
cd /home/ubuntu/arcane/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors on the new fields.

- [ ] **Step 6: Commit**

```bash
cd /home/ubuntu/arcane
git add frontend/src/lib/types/settings.type.ts "frontend/src/routes/(app)/environments/[id]/+page.svelte"
git commit -m "feat: add auto-update window fields to Settings type and env form"
```

---

## Task 6: Frontend — UI in JobsTab

**Files:**
- Modify: `frontend/src/routes/(app)/environments/[id]/components/JobsTab.svelte`

This is the main UI task. The new block sits inside the `{#if job.id === 'auto-update' && $formInputs.autoUpdate.value}` section, after the excluded-containers block.

- [ ] **Step 1: Add the window UI block**

Open `frontend/src/routes/(app)/environments/[id]/components/JobsTab.svelte`.

Find the closing `</div>` of the auto-update content block (the one that wraps the excluded containers section — it closes after the scrollarea). Immediately before that closing `</div>`, add:

```svelte
<!-- Time window section -->
<div class="border-border/20 space-y-3 border-t pt-3">
    <div class="flex items-center justify-between">
        <div class="space-y-0.5">
            <Label class="text-sm font-medium">{m.auto_update_window_enabled()}</Label>
        </div>
        <Switch bind:checked={$formInputs.autoUpdateWindowEnabled.value} />
    </div>

    {#if $formInputs.autoUpdateWindowEnabled.value}
        <!-- Time range pickers -->
        <div class="flex items-center gap-3">
            <div class="flex flex-1 flex-col gap-1">
                <Label class="text-xs">{m.auto_update_window_from()}</Label>
                <input
                    type="time"
                    class="border-input bg-background text-foreground focus-visible:ring-ring h-9 w-full rounded-md border px-3 py-1 text-sm shadow-sm focus-visible:ring-1 focus-visible:outline-none"
                    bind:value={$formInputs.autoUpdateWindowStart.value}
                />
            </div>
            <div class="flex flex-1 flex-col gap-1">
                <Label class="text-xs">{m.auto_update_window_to()}</Label>
                <input
                    type="time"
                    class="border-input bg-background text-foreground focus-visible:ring-ring h-9 w-full rounded-md border px-3 py-1 text-sm shadow-sm focus-visible:ring-1 focus-visible:outline-none"
                    bind:value={$formInputs.autoUpdateWindowEnd.value}
                />
            </div>
        </div>

        <!-- Day-of-week badges -->
        <div class="space-y-1.5">
            <Label class="text-xs">{m.auto_update_window_days()}</Label>
            <div class="flex flex-wrap gap-1.5">
                {#each [
                    { day: 1, label: 'Mon' },
                    { day: 2, label: 'Tue' },
                    { day: 3, label: 'Wed' },
                    { day: 4, label: 'Thu' },
                    { day: 5, label: 'Fri' },
                    { day: 6, label: 'Sat' },
                    { day: 0, label: 'Sun' }
                ] as { day: number; label: string } (day)}
                    {@const activeDays = $formInputs.autoUpdateWindowDays.value
                        .split(',')
                        .map((d: string) => d.trim())
                        .filter(Boolean)}
                    {@const isActive = activeDays.includes(String(day))}
                    <button
                        type="button"
                        onclick={() => {
                            const current = $formInputs.autoUpdateWindowDays.value
                                .split(',')
                                .map((d: string) => d.trim())
                                .filter(Boolean);
                            const dayStr = String(day);
                            const next = current.includes(dayStr)
                                ? current.filter((d: string) => d !== dayStr)
                                : [...current, dayStr].sort();
                            $formInputs.autoUpdateWindowDays.value = next.join(',');
                        }}
                        class="rounded-md border px-2.5 py-1 text-xs font-medium transition-colors {isActive
                            ? 'bg-primary text-primary-foreground border-primary'
                            : 'border-border/40 text-muted-foreground hover:bg-white/5'}"
                    >
                        {label}
                    </button>
                {/each}
            </div>
        </div>

        <!-- Advanced: cron expression (read-only when window active) -->
        <details class="space-y-1">
            <summary class="text-muted-foreground cursor-pointer text-xs select-none">
                {m.auto_update_window_cron_label()}
            </summary>
            <div class="mt-1.5 space-y-1">
                <input
                    type="text"
                    readonly
                    value="*/5 * * * * *"
                    class="border-input bg-muted text-muted-foreground h-9 w-full cursor-default rounded-md border px-3 py-1 font-mono text-sm"
                />
                <p class="text-muted-foreground text-xs">{m.auto_update_window_cron_hint()}</p>
            </div>
        </details>
    {/if}
</div>
```

- [ ] **Step 2: Verify TypeScript / Svelte compilation**

```bash
cd /home/ubuntu/arcane/frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /home/ubuntu/arcane
git add "frontend/src/routes/(app)/environments/[id]/components/JobsTab.svelte"
git commit -m "feat: add time-window UI to auto-update job card"
```

---

## Task 7: Manual smoke test + final wiring check

- [ ] **Step 1: Run the full backend test suite**

```bash
cd /home/ubuntu/arcane/backend && go test ./... 2>&1 | tail -20
```

Expected: all tests PASS (no new failures).

- [ ] **Step 2: Start the dev frontend**

```bash
cd /home/ubuntu/arcane/frontend && pnpm dev 2>&1 &
```

Open the environment settings page → Jobs tab → enable Auto Update → verify the window toggle appears below the excluded-containers block.

- [ ] **Step 3: Enable window and save**

- Toggle "Restrict updates to a time window" ON
- Set From: `02:00`, To: `04:00`
- Leave all days checked
- Click Save
- Reload page → verify the values persist

- [ ] **Step 4: Verify scheduler picks up the change**

Check backend logs. After saving, you should see a reschedule log entry for `auto-update` job (the existing reschedule trigger fires on `autoUpdateWindowEnabled` change).

- [ ] **Step 5: Final commit (if any tweaks made)**

```bash
cd /home/ubuntu/arcane
git add -p
git commit -m "fix: post-review tweaks to auto-update window"
```

---

## Quick Reference — run all backend tests

```bash
cd /home/ubuntu/arcane/backend && go test ./pkg/scheduler/... -v -run TestAutoUpdateJob
```

## Quick Reference — build everything

```bash
cd /home/ubuntu/arcane/backend && go build ./... && echo "backend OK"
cd /home/ubuntu/arcane/frontend && npx tsc --noEmit && echo "frontend OK"
```
