<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';
	import MetricRing from '#lib/components/metric-ring.svelte';

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

	const percent = $derived(type === 'cpu' ? value : memoryPercent);
</script>

{#if stopped}
	<div class="text-xs text-muted-foreground">{m.common_na()}</div>
{:else if loading}
	<div class="flex items-center gap-2">
		<div class="size-[26px] shrink-0 animate-pulse rounded-full bg-muted"></div>
		<div class="h-3 w-16 animate-pulse rounded bg-muted"></div>
	</div>
{:else if type === 'memory' && memoryFormatted}
	<div class="flex items-center gap-2">
		<MetricRing {percent} variant={type} />
		<span class="text-xs font-medium text-foreground tabular-nums">
			{memoryFormatted}
		</span>
	</div>
{:else if type === 'cpu' && value !== undefined}
	<div class="flex items-center gap-2">
		<MetricRing {percent} variant={type} />
		<span class="text-xs font-medium text-foreground tabular-nums">
			{value.toFixed(1)}%
		</span>
	</div>
{:else}
	<div class="text-xs text-muted-foreground">—</div>
{/if}
