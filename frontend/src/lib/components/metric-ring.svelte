<script module lang="ts">
	export type MetricRingVariant = 'cpu' | 'memory' | 'disk' | 'backup';
	export type MetricRingTone = 'default' | 'warning' | 'destructive';

	const variantClass: Record<MetricRingVariant, string> = {
		cpu: 'text-metric-cpu',
		memory: 'text-metric-memory',
		disk: 'text-metric-disk',
		backup: 'text-metric-backup'
	};

	const toneClass: Record<Exclude<MetricRingTone, 'default'>, string> = {
		warning: 'text-warning',
		destructive: 'text-destructive'
	};

	export function metricColorClass(variant: MetricRingVariant, tone: MetricRingTone): string {
		return tone === 'default' ? variantClass[variant] : toneClass[tone];
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		percent?: number;
		variant: MetricRingVariant;
		tone?: MetricRingTone;
		size?: number;
		stroke?: number;
		children?: Snippet;
	}

	let { percent, variant, tone = 'default', size = 26, stroke = 3.5, children }: Props = $props();

	const clampedPercent = $derived(percent === undefined ? 0 : Math.max(0, Math.min(100, percent)));
	const radius = $derived((size - stroke) / 2);
	const circumference = $derived(2 * Math.PI * radius);
	const dashOffset = $derived(circumference * (1 - clampedPercent / 100));
</script>

<span class="relative inline-flex size-(--ring-size) shrink-0 items-center justify-center" style:--ring-size="{size}px">
	<svg width={size} height={size} viewBox="0 0 {size} {size}" class="absolute inset-0 -rotate-90" aria-hidden="true">
		<circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="currentColor" stroke-width={stroke} class="text-border" />
		{#if clampedPercent > 0}
			<circle
				cx={size / 2}
				cy={size / 2}
				r={radius}
				fill="none"
				stroke="currentColor"
				stroke-width={stroke}
				stroke-linecap={clampedPercent >= 3 ? 'round' : 'butt'}
				stroke-dasharray={circumference}
				stroke-dashoffset={dashOffset}
				class={['transition-stroke-dashoffset duration-300 ease-out', metricColorClass(variant, tone)]}
			/>
		{/if}
	</svg>
	{#if children}
		<span class="relative flex items-center leading-none">{@render children()}</span>
	{/if}
</span>
