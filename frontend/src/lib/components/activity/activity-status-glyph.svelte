<script lang="ts">
	import type { ActivityStatus } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';

	import { activityStatusLabel } from './activity-labels';

	let {
		status,
		size = 'default',
		class: className
	}: {
		status: ActivityStatus;
		size?: 'sm' | 'default';
		class?: string;
	} = $props();
</script>

<span
	role="img"
	aria-label={activityStatusLabel(status)}
	class={cn('grid shrink-0 place-items-center', size === 'sm' ? 'size-3.5' : 'size-4.5', className)}
>
	{#if status === 'running'}
		<span
			class={cn(
				'animate-spin rounded-full border-2 border-info/25 border-t-info motion-reduce:animate-none',
				size === 'sm' ? 'size-3' : 'size-3.5'
			)}
		></span>
	{:else if status === 'queued'}
		<span class={cn('rounded-full border-2 border-dashed border-warning', size === 'sm' ? 'size-2.5' : 'size-3')}></span>
	{:else if status === 'cancelled'}
		<span class="h-0.5 w-2.5 rounded-full bg-muted-foreground/70"></span>
	{:else}
		<span
			class={cn(
				'rounded-full',
				size === 'sm' ? 'size-1.5' : 'size-2',
				status === 'success' && 'bg-success',
				status === 'failed' && 'bg-destructive'
			)}
		></span>
	{/if}
</span>
