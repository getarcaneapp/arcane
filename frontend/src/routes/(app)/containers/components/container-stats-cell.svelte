<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages';
	import bytes from '$lib/utils/bytes';

	interface Props {
		value?: number;
		limit?: number;
		loading?: boolean;
		stopped?: boolean;
		type: 'cpu' | 'memory';
	}

	let { value, limit, loading = false, stopped = false, type }: Props = $props();

	const memoryPercent = $derived.by(() => {
		if (type !== 'memory' || !value || !limit || limit === 0) return undefined;
		return (value / limit) * 100;
	});

	const memoryFormatted = $derived.by(() => {
		if (type !== 'memory' || value === undefined) return undefined;
		return bytes.format(value, { unitSeparator: ' ' });
	});
</script>

{#if stopped}
	<div class="text-muted-foreground text-xs">{m.common_na()}</div>
{:else if loading}
	<div class="flex items-center gap-2">
		<div class="bg-muted h-1.5 flex-1 animate-pulse rounded-full"></div>
		<div class="bg-muted h-3 w-16 animate-pulse rounded"></div>
	</div>
{:else if type === 'memory' && memoryFormatted}
	<div class="flex items-center gap-2">
		{#if memoryPercent !== undefined}
			<Progress value={memoryPercent} max={100} class="h-1.5 flex-1" />
		{/if}
		<span class="text-foreground min-w-16 text-right text-xs font-medium tabular-nums">
			{memoryFormatted}
		</span>
	</div>
{:else if type === 'cpu' && value !== undefined}
	<div class="flex items-center gap-2">
		<Progress {value} max={100} class="h-1.5 flex-1" />
		<span class="text-foreground min-w-10 text-right text-xs font-medium tabular-nums">
			{value.toFixed(1)}%
		</span>
	</div>
{:else}
	<div class="text-muted-foreground text-xs">—</div>
{/if}
