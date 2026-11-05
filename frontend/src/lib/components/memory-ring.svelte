<script lang="ts">
	import MetricRing, { type MetricRingTone } from '#lib/components/metric-ring.svelte';
	import { bytes } from '#lib/utils/formatting.js';

	interface Props {
		used: number;
		total?: number;
		size?: number;
		tone?: MetricRingTone;
	}

	let { used, total, size = 38, tone = 'default' }: Props = $props();

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
	<MetricRing {percent} variant="memory" {tone} {size} stroke={3}>
		<span class="flex flex-col items-center gap-px leading-none">
			<span class={['font-medium text-foreground tabular-nums', size >= 44 ? 'text-xs-plus' : 'text-2xs']}>{label.amount}</span>
			<span class={['text-muted-foreground tabular-nums', size >= 44 ? 'text-xs' : 'text-3xs']}>{label.unit}</span>
		</span>
	</MetricRing>
</span>
