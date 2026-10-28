<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { Activity, ActivityStatus } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';
	import { formatDateTime } from '#lib/utils/formatting.js';

	import ActivityActions from './activity-actions.svelte';
	import ActivityElapsed from './activity-elapsed.svelte';
	import { isActiveActivityStatus } from './activity-groups';
	import { activityDurationLabel, activityRunStatusLabel, activityStatusLabel, activityTypeLabel } from './activity-labels';
	import ActivityStatusGlyph from './activity-status-glyph.svelte';

	let {
		activity,
		status,
		target,
		batch = false,
		compact = false
	}: {
		activity: Activity;
		status: ActivityStatus;
		target: string;
		batch?: boolean;
		/** Narrow surfaces (tray, mobile) stack the facts in two columns and shrink the title. */
		compact?: boolean;
	} = $props();

	const isActive = $derived(isActiveActivityStatus(status));
	const environment = $derived(activity.sourceEnvironmentName || activity.sourceEnvironmentId || activity.environmentId);
	const statusLabel = $derived(activity.type === 'job_run' ? activityRunStatusLabel(activity) : activityStatusLabel(status));
	const startedBy = $derived(activity.startedBy?.displayName || activity.startedBy?.username);
</script>

<header class={cn('flex flex-col gap-3 border-b border-border/50', compact ? 'px-4 pt-3 pb-4' : 'px-6 pt-5 pb-5')}>
	<div
		class={cn(
			'flex items-center gap-1.5 text-xs font-semibold',
			status === 'running' && 'text-info',
			status === 'queued' && 'text-warning',
			status === 'success' && 'text-success',
			status === 'failed' && 'text-destructive',
			status === 'cancelled' && 'text-muted-foreground'
		)}
	>
		<ActivityStatusGlyph {status} size="sm" />
		{statusLabel}
	</div>

	<h3 class={cn('leading-tight font-semibold text-foreground', compact ? 'text-base' : 'text-2xl')}>
		{activityTypeLabel(activity.type)}
		{#if target}<span class="font-normal text-muted-foreground">{target}</span>{/if}
	</h3>
	{#if activity.error}
		<p class="-mt-1 text-sm break-words text-destructive">{activity.error}</p>
	{/if}

	<dl class={cn('grid gap-x-4 gap-y-2.5 text-sm', compact ? 'grid-cols-2' : 'grid-cols-2 md:grid-cols-4')}>
		<div class="min-w-0">
			<dt class="text-2xs text-muted-foreground">{m.resource_environment_cap()}</dt>
			<dd class="truncate font-medium">{environment}</dd>
		</div>
		<div class="min-w-0">
			<dt class="text-2xs text-muted-foreground">{m.activity_started_by_label()}</dt>
			<dd class="truncate font-medium">{startedBy || m.common_na()}</dd>
		</div>
		<div class="min-w-0">
			<dt class="text-2xs text-muted-foreground">{m.common_started()}</dt>
			<dd class="truncate font-medium tabular-nums">
				{activity.startedAt
					? formatDateTime(activity.startedAt, { dateStyle: 'month-day', includeSeconds: true })
					: m.common_na()}
			</dd>
		</div>
		<div class="min-w-0">
			<dt class="text-2xs text-muted-foreground">{isActive ? m.activity_elapsed() : m.duration()}</dt>
			<dd class="truncate font-medium tabular-nums">
				{#if isActive}
					<ActivityElapsed {activity} />
				{:else}
					{activityDurationLabel(activity)}
				{/if}
			</dd>
		</div>
	</dl>

	<ActivityActions {activity} {status} canCancel={!batch} />
</header>
