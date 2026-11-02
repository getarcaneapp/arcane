<script lang="ts">
	import MetricRing from '#lib/components/metric-ring.svelte';
	import { bytes } from '#lib/utils/formatting.js';

	interface Props {
		used: number;
		total?: number;
	}

	let { used, total }: Props = $props();

	const percent = $derived(!used || !total ? undefined : (used / total) * 100);
	const formatted = $derived(bytes.format(used, { decimalPlaces: 1, unitSeparator: ' ' }) ?? '');

	const label = $derived.by(() => {
		const [amountText = '', unit = ''] = formatted.split(' ');
		const amount = Number.parseFloat(amountText);
		return { amount: amount >= 100 ? Math.round(amount).toString() : amountText, unit };
	});

	const title = $derived(percent === undefined ? formatted : `${formatted} (${Math.round(percent)}%)`);
</script>

<span class="inline-flex" {title}>
	<MetricRing {percent} variant="memory" size={38} stroke={3}>
		<span class="flex flex-col items-center leading-none">
			<span class="text-2xs font-medium text-foreground tabular-nums">{label.amount}</span>
			<span class="text-3xs text-muted-foreground">{label.unit}</span>
		</span>
	</MetricRing>
</span>
