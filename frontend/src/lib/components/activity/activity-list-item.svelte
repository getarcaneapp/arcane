<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { ArrowDownIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { formatDistanceToNow } from 'date-fns';
	import type { Activity, ActivityStatus } from '$lib/types/activity.type';
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
	const hasProgress = $derived(typeof activity.progress === 'number');
	const progressValue = $derived(Math.round(activity.progress ?? 0));
	const isActive = $derived(activity.status === 'running' || activity.status === 'queued');
	// Resource-less activities (e.g. prunes) promote the type label instead.
	const resourceLabel = $derived(activity.resourceName || activity.resourceId || '');
	const targetName = $derived(resourceLabel || activityTypeLabel(activity.type));
	const subtitle = $derived(activity.latestMessage || activity.step || m.activity_no_message());
	const sourceEnvironmentName = $derived(
		activity.sourceEnvironmentName || activity.sourceEnvironmentId || activity.environmentId
	);
	const startedByName = $derived(activity.startedBy?.displayName || activity.startedBy?.username);

	const referenceDate = $derived(activity.endedAt || activity.startedAt);
	const relativeTime = $derived(referenceDate ? formatDistanceToNow(new Date(referenceDate), { addSuffix: true }) : '');

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
	<div class="min-w-0 space-y-1.5">
		<div class="flex min-w-0 items-start justify-between gap-3">
			<div class="min-w-0 flex-1">
				<div class="flex min-w-0 items-center gap-2">
					<span class="truncate text-sm font-semibold text-foreground">{targetName}</span>
					{#if relativeTime}
						<span class="shrink-0 text-[11px] text-muted-foreground/70">· {relativeTime}</span>
					{/if}
				</div>
				{#if resourceLabel}
					<div class="truncate text-xs text-muted-foreground">{activityTypeLabel(activity.type)}</div>
				{/if}
				{#if !child}
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

		<div class="space-y-1.5">
			<div class="line-clamp-2 text-xs leading-relaxed text-muted-foreground">{subtitle}</div>
			{#if isActive && !expanded}
				<div class="flex items-center gap-2">
					<Progress value={hasProgress ? progressValue : 100} indeterminate={!hasProgress} class="h-1.5 rounded-full" />
					<span class="w-9 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">
						{#if hasProgress}
							{m.activity_progress_percent({ progress: progressValue })}
						{:else}
							{m.common_live()}
						{/if}
					</span>
				</div>
			{/if}
		</div>
	</div>

	<div class="mt-1 flex size-6 shrink-0 items-center justify-center text-muted-foreground">
		<ArrowDownIcon class={cn('size-4 transition-transform duration-200', expanded && 'rotate-180')} aria-hidden="true" />
	</div>
</div>
