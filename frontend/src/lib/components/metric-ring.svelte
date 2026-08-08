<script module lang="ts">
	export type MetricRingVariant = 'cpu' | 'memory' | 'disk';

	let uidCounter = 0;
	function nextUid() {
		return ++uidCounter;
	}

	const gradientStops: Record<MetricRingVariant, [string, string]> = {
		cpu: ['oklch(0.75 0.18 35)', 'oklch(0.65 0.24 25)'],
		memory: ['oklch(0.72 0.18 300)', 'oklch(0.58 0.25 290)'],
		disk: ['oklch(0.78 0.15 195)', 'oklch(0.62 0.16 220)']
	};
</script>

<script lang="ts">
	interface Props {
		/** 0-100; values outside the range are clamped. */
		percent?: number;
		variant: MetricRingVariant;
		size?: number;
		stroke?: number;
	}

	let { percent, variant, size = 26, stroke = 3.5 }: Props = $props();

	const clampedPercent = $derived(percent === undefined ? 0 : Math.max(0, Math.min(100, percent)));
	const radius = $derived((size - stroke) / 2);
	const circumference = $derived(2 * Math.PI * radius);
	const dashOffset = $derived(circumference * (1 - clampedPercent / 100));

	const uid = nextUid();
	const gradientId = $derived(`arcane-ring-${variant}-${uid}`);
	const stops = $derived(gradientStops[variant]);
</script>

<svg width={size} height={size} viewBox="0 0 {size} {size}" class="shrink-0 -rotate-90" aria-hidden="true">
	<defs>
		<linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
			<stop offset="0%" stop-color={stops[0]} />
			<stop offset="100%" stop-color={stops[1]} />
		</linearGradient>
	</defs>
	<circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="currentColor" stroke-width={stroke} class="text-muted/40" />
	<circle
		cx={size / 2}
		cy={size / 2}
		r={radius}
		fill="none"
		stroke="url(#{gradientId})"
		stroke-width={stroke}
		stroke-linecap="round"
		stroke-dasharray={circumference}
		stroke-dashoffset={dashOffset}
		style="transition: stroke-dashoffset 300ms ease-out;"
	/>
</svg>
