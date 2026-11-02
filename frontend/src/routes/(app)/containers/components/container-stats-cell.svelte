<script lang="ts">
	import MemoryRing from '#lib/components/memory-ring.svelte';
	import MetricRing from '#lib/components/metric-ring.svelte';
	import { m } from '#lib/paraglide/messages.js';

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

{#if stopped}
	<div class="text-xs text-muted-foreground">{m.common_na()}</div>
{:else if unavailable && value === undefined}
	<div class="text-xs text-muted-foreground">{m.common_unavailable()}</div>
{:else if loading}
	<div class="size-9.5 shrink-0 animate-pulse rounded-full bg-muted"></div>
{:else if type === 'cpu' && value !== undefined}
	<span class="inline-flex">
		<MetricRing percent={value} variant="cpu" size={38} stroke={3}>
			<span class="text-2xs font-medium text-foreground tabular-nums">{Math.round(value)}%</span>
		</MetricRing>
	</span>
{:else if type === 'memory' && value !== undefined}
	<MemoryRing used={value} total={limit} />
{:else}
	<div class="text-xs text-muted-foreground">—</div>
{/if}
