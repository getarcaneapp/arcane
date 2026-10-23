<script lang="ts">
	import MetricRing from '#lib/components/metric-ring.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';

	interface Props {
		value?: number;
		limit?: number;
		loading?: boolean;
		stopped?: boolean;
		unavailable?: boolean;
		type: 'cpu' | 'memory';
	}

	let { value, limit, loading = false, stopped = false, unavailable = false, type }: Props = $props();

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
{:else if unavailable && value === undefined}
	<div class="text-xs text-muted-foreground">{m.common_unavailable()}</div>
{:else if loading}
	<div class="size-9.5 shrink-0 animate-pulse rounded-full bg-muted"></div>
{:else if percent !== undefined}
	<span class="inline-flex" title={memoryFormatted}>
		<MetricRing {percent} variant={type} size={38} stroke={3}>
			<span class="text-2xs font-medium text-foreground tabular-nums">{Math.round(percent)}%</span>
		</MetricRing>
	</span>
{:else if memoryFormatted}
	<div class="text-xs font-medium text-foreground tabular-nums">{memoryFormatted}</div>
{:else}
	<div class="text-xs text-muted-foreground">—</div>
{/if}
