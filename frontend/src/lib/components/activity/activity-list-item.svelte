<script lang="ts">
	import { Temporal } from 'temporal-polyfill';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Badge } from '#lib/components/ui/badge';
	import { ArrowDownIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { cn } from '#lib/utils';
	import { formatRelativeTime, formatDateTime, parseInstant } from '#lib/utils/formatting';
	import type { Activity, ActivityStatus } from '#lib/types/activity.type';
	import { activityStatusLabel, activityStatusVariant, activityTypeIcon, activityTypeLabel } from './activity-labels';

	let {
		activity,
		expanded = false,
		child = false
	}: {
		activity: Activity;
		expanded?: boolean;
		/** Compact variant for rows nested inside a batch group. */
		child?: boolean;
	} = $props();

	const IconComponent = $derived(activityTypeIcon(activity.type));
	const isActive = $derived(activity.status === 'running' || activity.status === 'queued');
	const resourceLabel = $derived(activity.resourceName || activity.resourceId || '');
	const subtitle = $derived(activity.latestMessage || m.activity_no_message());
	const sourceEnvironmentName = $derived(
		activity.sourceEnvironmentName || activity.sourceEnvironmentId || activity.environmentId
	);
	const startedByName = $derived(activity.startedBy?.displayName || activity.startedBy?.username);

	const referenceDate = $derived(activity.endedAt || activity.startedAt);
	const relativeTime = $derived(formatRelativeTime(referenceDate));

	function statusAccentClass(status: ActivityStatus): string {
		switch (status) {
			case 'failed':
				return 'bg-red-500';
			case 'running':
				return 'bg-blue-500';
			case 'queued':
				return 'bg-amber-500';
			case 'success':
				return 'bg-emerald-500';
			case 'cancelled':
				return 'bg-muted-foreground/40';
		}
	}
	function formatDateTimeInternal(value?: string): string {
		if (!value) {
			return m.common_na();
		}
		return formatDateTime(value, {
			dateStyle: 'month-day',
			includeSeconds: true
		});
	}

	function formatDurationInternal(value: Activity | null): string {
		const startedAt = parseInstant(value?.startedAt);
		const durationMs = value?.durationMs ?? (startedAt ? startedAt.until(Temporal.Now.instant()).total('milliseconds') : 0);
		if (!durationMs || Number.isNaN(durationMs)) {
			return m.common_na();
		}
		if (durationMs < 1000) {
			return m.activity_duration_ms({ ms: Math.max(0, Math.round(durationMs)) });
		}

		const totalSeconds = Math.round(durationMs / 1000);
		if (totalSeconds < 60) {
			return m.activity_duration_seconds({ seconds: totalSeconds });
		}

		const minutes = Math.floor(totalSeconds / 60);
		const seconds = totalSeconds % 60;
		return m.activity_duration_minutes({ minutes, seconds });
	}
</script>

<div
	class={cn(
		'group relative grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 border-b border-border/40 text-left transition-colors last:border-b-0 hover:bg-muted/30',
		child ? 'px-3 py-2' : 'px-4 py-3',
		expanded && 'bg-muted/40'
	)}
>
	<span
		aria-hidden="true"
		class={cn(
			'absolute top-2 bottom-2 left-0 rounded-r-full transition-all',
			statusAccentClass(activity.status),
			expanded ? 'w-1' : 'w-0.5'
		)}
	></span>

	<div
		class={cn(
			'mt-0.5 flex items-center justify-center rounded-md bg-muted/80 text-muted-foreground',
			child ? 'size-6' : 'size-8',
			isActive && 'bg-primary/10 text-primary',
			expanded && 'bg-primary/10 text-primary'
		)}
	>
		<IconComponent class={cn(child ? 'size-3.5' : 'size-4')} aria-hidden="true" />
	</div>
	<div class={cn('flex min-w-0 flex-col gap-1.5', isActive && 'pr-8')}>
		<div class="flex min-w-0 items-start justify-between gap-3">
			<div class="min-w-0 flex-1">
				<div class="flex min-w-0 items-center gap-2">
					<span class="truncate text-sm font-semibold text-foreground">{activityTypeLabel(activity.type)}</span>
					{#if relativeTime}
						<span class="shrink-0 text-[11px] text-muted-foreground/70">· {relativeTime}</span>
					{/if}
				</div>
				{#if resourceLabel}
					<div class="truncate text-xs text-muted-foreground">{resourceLabel}</div>
				{/if}
				{#if !child || expanded}
					<div class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11px] text-muted-foreground/80">
						{#if sourceEnvironmentName}
							<span class="truncate">{sourceEnvironmentName}</span>
						{/if}
						{#if startedByName}
							<span class="text-muted-foreground/50">·</span>
							<span class="truncate">{m.activity_started_by({ user: startedByName })}</span>
						{/if}
					</div>
				{/if}
			</div>
			<Badge variant={activityStatusVariant(activity.status)} size="sm">{activityStatusLabel(activity.status)}</Badge>
		</div>

		{#if expanded}
			<div class="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
				<span
					>{m.common_started()}
					<span class="text-foreground tabular-nums">{formatDateTimeInternal(activity.startedAt)}</span></span
				>
				<span
					>{m.common_finished()}
					<span class="text-foreground tabular-nums">{formatDateTimeInternal(activity.endedAt)}</span></span
				>
				<span>{m.duration()} <span class="text-foreground tabular-nums">{formatDurationInternal(activity)}</span></span>
			</div>
		{/if}

		{#if !expanded}
			<div class="flex flex-col gap-1.5">
				<div class="line-clamp-2 text-xs leading-relaxed text-muted-foreground">{subtitle}</div>
				{#if isActive}
					<Progress value={100} indeterminate class="h-1.5 rounded-full" />
				{/if}
			</div>
		{/if}
	</div>

	<div class="mt-1 flex size-6 shrink-0 items-center justify-center text-muted-foreground">
		<ArrowDownIcon class={cn('size-4 transition-transform duration-200', expanded && 'rotate-180')} aria-hidden="true" />
	</div>
</div>
