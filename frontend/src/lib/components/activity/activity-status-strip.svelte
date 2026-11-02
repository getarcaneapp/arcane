<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { Activity } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';

	import { activityStatusLabel } from './activity-labels';

	// Batches beyond this many members collapse the remainder into a count.
	const MAX_SEGMENTS = 24;

	let { items, done, total }: { items: Activity[]; done: number; total: number } = $props();

	const visible = $derived(items.slice(0, MAX_SEGMENTS));
	const overflow = $derived(items.length - visible.length);
</script>

<span class="flex min-w-0 items-center gap-1" role="img" aria-label={m.activity_batch_done_of_total({ done, total })}>
	{#each visible as item (item.id)}
		<span
			title={`${item.resourceName || item.resourceId || ''} ${activityStatusLabel(item.status)}`.trim()}
			class={cn(
				'size-2.5 shrink-0 rounded-xs',
				item.status === 'success' && 'bg-success',
				item.status === 'failed' && 'bg-destructive',
				item.status === 'running' && 'animate-pulse bg-info motion-reduce:animate-none',
				item.status === 'queued' && 'border border-warning',
				item.status === 'cancelled' && 'bg-muted-foreground/40'
			)}
		></span>
	{/each}
	{#if overflow > 0}
		<span class="text-3xs text-muted-foreground tabular-nums">+{overflow}</span>
	{/if}
</span>
