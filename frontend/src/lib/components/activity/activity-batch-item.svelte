<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { ArrowDownIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { ActivityBatchGroup, ActivityStatus } from '$lib/types/activity.type';
	import { activityStatusLabel, activityStatusVariant, activityTypeIcon, activityTypeLabel } from './activity-labels';

	let {
		group,
		expanded = false
	}: {
		group: ActivityBatchGroup;
		expanded?: boolean;
	} = $props();

	// Batch groups are constructed with at least one member.
	const leadActivity = $derived(group.items[0]!);
	const IconComponent = $derived(activityTypeIcon(leadActivity.type));
	const isActive = $derived(group.status === 'running' || group.status === 'queued');

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
		'group relative grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 border-b border-border/40 px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-muted/30',
		expanded && 'bg-muted/40'
	)}
>
	<span
		aria-hidden="true"
		class={cn(
			'absolute top-2 bottom-2 left-0 rounded-r-full transition-all',
			statusAccentClass(group.status),
			expanded ? 'w-1' : 'w-0.5'
		)}
	></span>

	<div
		class={cn(
			'mt-0.5 flex size-8 items-center justify-center rounded-md bg-muted/80 text-muted-foreground',
			(isActive || expanded) && 'bg-primary/10 text-primary'
		)}
	>
		<IconComponent class="size-4" aria-hidden="true" />
	</div>
	<div class="min-w-0 space-y-1.5">
		<div class="flex min-w-0 items-start justify-between gap-3">
			<div class="min-w-0 flex-1">
				<div class="flex min-w-0 items-center gap-2">
					<span class="truncate text-sm font-semibold text-foreground">{activityTypeLabel(leadActivity.type)}</span>
					<span class="shrink-0 text-[11px] text-muted-foreground/70">· {m.activity_batch_items({ count: group.total })}</span>
				</div>
				<div class="flex min-w-0 flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
					<span>{m.activity_batch_done_of_total({ done: group.done, total: group.total })}</span>
					{#if group.failed > 0}
						<span class="text-muted-foreground/50">·</span>
						<span class="text-red-500">{m.activity_batch_failed_count({ count: group.failed })}</span>
					{/if}
				</div>
			</div>
			<Badge variant={activityStatusVariant(group.status)} size="sm">{activityStatusLabel(group.status)}</Badge>
		</div>

		{#if isActive}
			<div class="flex items-center gap-2">
				<Progress value={group.progress ?? 0} class="h-1.5 rounded-full" />
				<span class="w-9 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">
					{m.activity_progress_percent({ progress: group.progress ?? 0 })}
				</span>
			</div>
		{/if}
	</div>

	<div class="mt-1 flex size-6 shrink-0 items-center justify-center text-muted-foreground">
		<ArrowDownIcon class={cn('size-4 transition-transform duration-200', expanded && 'rotate-180')} aria-hidden="true" />
	</div>
</div>
