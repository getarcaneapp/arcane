<script lang="ts">
	import type { ClassValue } from 'svelte/elements';
	import { cn } from '#lib/utils.js';
	import type { IconType } from '#lib/icons/index.js';

	interface Props {
		title: string;
		value: string;
		label: string;
		icon: IconType;
		meterValue?: number | null;
		labelClass?: ClassValue;
	}

	let { title, value, label, icon: Icon, meterValue = null, labelClass }: Props = $props();

	const HIGH_USAGE_THRESHOLD = 90;

	const meterPercent = $derived.by(() => {
		if (meterValue === null) return 0;
		return Math.max(0, Math.min(100, meterValue));
	});

	const meterIsHigh = $derived(meterPercent >= HIGH_USAGE_THRESHOLD);
</script>

<div class="min-w-0 px-2.5 py-2.5">
	<div class="flex items-start justify-between gap-2">
		<p class="flex items-center gap-1 text-3xs font-semibold tracking-wide text-foreground/70 uppercase">
			<Icon class="size-3.5" />
			{title}
		</p>
		<p class="text-base font-semibold tracking-tight tabular-nums">{value}</p>
	</div>
	<p class={cn('mt-0.5 text-2xs text-muted-foreground/90', labelClass)}>{label}</p>

	{#if meterValue !== null}
		<div class="mt-2.5">
			<div class="relative h-1.5 overflow-hidden rounded-full bg-muted/45">
				<div class="pointer-events-none absolute inset-0">
					{#each [25, 50, 75] as tick (tick)}
						<span class="absolute top-0 left-(--tick) h-full w-px bg-foreground/15 opacity-60" style={`--tick: ${tick}%`}></span>
					{/each}
				</div>
				<div
					class={cn(
						'absolute inset-y-0 left-0 w-(--meter) rounded-full bg-gradient-to-r transition-all duration-700 ease-out motion-reduce:transition-none',
						meterIsHigh ? 'from-primary/65 via-primary to-destructive' : 'from-primary/65 to-primary'
					)}
					style={`--meter: ${meterPercent}%`}
				></div>
				{#if meterPercent > 0}
					<div
						class={cn(
							'absolute top-1/2 left-(--meter) size-2 -translate-x-1 -translate-y-1/2 rounded-full ring-2 ring-background transition-all duration-700 ease-out motion-reduce:transition-none',
							meterIsHigh ? 'bg-destructive' : 'bg-primary'
						)}
						style={`--meter: ${meterPercent}%`}
					></div>
				{/if}
			</div>
		</div>
	{/if}
</div>
