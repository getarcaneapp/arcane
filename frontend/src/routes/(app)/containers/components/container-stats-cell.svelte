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
		return bytes.format(value, { decimalPlaces: 1, unitSeparator: ' ' }) ?? undefined;
	});

	const memoryLabel = $derived.by(() => {
		if (!memoryFormatted) return undefined;
		const [amountText = '', unit = ''] = memoryFormatted.split(' ');
		const amount = Number.parseFloat(amountText);
		return { amount: amount >= 100 ? Math.round(amount).toString() : amountText, unit };
	});

	const memoryTitle = $derived.by(() => {
		if (!memoryFormatted) return undefined;
		if (memoryPercent === undefined) return memoryFormatted;
		return `${memoryFormatted} (${Math.round(memoryPercent)}%)`;
	});
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
{:else if type === 'memory' && memoryLabel}
	<span class="inline-flex" title={memoryTitle}>
		<MetricRing percent={memoryPercent} variant="memory" size={38} stroke={3}>
			<span class="flex flex-col items-center leading-none">
				<span class="text-2xs font-medium text-foreground tabular-nums">{memoryLabel.amount}</span>
				<span class="text-3xs text-muted-foreground">{memoryLabel.unit}</span>
			</span>
		</MetricRing>
	</span>
{:else}
	<div class="text-xs text-muted-foreground">—</div>
{/if}
