<script lang="ts">
	import MemoryRing from '#lib/components/memory-ring.svelte';
	import MetricRing from '#lib/components/metric-ring.svelte';
	import { m } from '#lib/paraglide/messages.js';

	import { getUsageTone } from '../container-table.helpers';

	interface Props {
		value?: number;
		limit?: number;
		loading?: boolean;
		stopped?: boolean;
		unavailable?: boolean;
		type: 'cpu' | 'memory';
	}

	let { value, limit, loading = false, stopped = false, unavailable = false, type }: Props = $props();
</script>

<div class="mx-auto flex size-stat-ring shrink-0 items-center justify-center whitespace-nowrap">
	{#if stopped}
		<span class="text-xs text-muted-foreground">{m.common_na()}</span>
	{:else if unavailable && value === undefined}
		<span class="text-xs text-muted-foreground">{m.common_unavailable()}</span>
	{:else if loading}
		<span class="size-full animate-pulse rounded-full bg-muted"></span>
	{:else if type === 'cpu' && value !== undefined}
		<MetricRing percent={value} variant="cpu" tone={getUsageTone(value)} size={38} stroke={3}>
			<span class="text-2xs font-medium text-foreground tabular-nums">{Math.round(value)}%</span>
		</MetricRing>
	{:else if type === 'memory' && value !== undefined}
		<MemoryRing used={value} total={limit} tone={getUsageTone(limit ? (value / limit) * 100 : 0)} />
	{:else}
		<span class="text-xs text-muted-foreground">—</span>
	{/if}
</div>
