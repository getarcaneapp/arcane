<script module lang="ts">
	let uidCounter = 0;
	function nextUid() {
		return ++uidCounter;
	}
</script>

<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { bytes } from '$lib/utils/formatting';

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
	const clampedPercent = $derived(percent === undefined ? 0 : Math.max(0, Math.min(100, percent)));

	const size = 26;
	const stroke = 3.5;
	const radius = (size - stroke) / 2;
	const circumference = 2 * Math.PI * radius;
	const dashOffset = $derived(circumference * (1 - clampedPercent / 100));

	const uid = nextUid();
	const gradientId = $derived(`arcane-ring-${type}-${uid}`);
</script>

{#snippet ring()}
	<svg width={size} height={size} viewBox="0 0 {size} {size}" class="shrink-0 -rotate-90" aria-hidden="true">
		<defs>
			{#if type === 'cpu'}
				<linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
					<stop offset="0%" stop-color="oklch(0.75 0.18 35)" />
					<stop offset="100%" stop-color="oklch(0.65 0.24 25)" />
				</linearGradient>
			{:else}
				<linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
					<stop offset="0%" stop-color="oklch(0.72 0.18 300)" />
					<stop offset="100%" stop-color="oklch(0.58 0.25 290)" />
				</linearGradient>
			{/if}
		</defs>
		<circle
			cx={size / 2}
			cy={size / 2}
			r={radius}
			fill="none"
			stroke="currentColor"
			stroke-width={stroke}
			class="text-muted/40"
		/>
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
{/snippet}

{#if stopped}
	<div class="text-muted-foreground text-xs">{m.common_na()}</div>
{:else if loading}
	<div class="flex items-center gap-2">
		<div class="bg-muted size-[26px] shrink-0 animate-pulse rounded-full"></div>
		<div class="bg-muted h-3 w-16 animate-pulse rounded"></div>
	</div>
{:else if type === 'memory' && memoryFormatted}
	<div class="flex items-center gap-2">
		{@render ring()}
		<span class="text-foreground text-xs font-medium tabular-nums">
			{memoryFormatted}
		</span>
	</div>
{:else if type === 'cpu' && value !== undefined}
	<div class="flex items-center gap-2">
		{@render ring()}
		<span class="text-foreground text-xs font-medium tabular-nums">
			{value.toFixed(1)}%
		</span>
	</div>
{:else}
	<div class="text-muted-foreground text-xs">—</div>
{/if}
