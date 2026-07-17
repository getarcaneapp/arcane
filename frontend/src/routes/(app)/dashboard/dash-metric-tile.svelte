<script lang="ts">
	import type { ClassValue } from 'svelte/elements';
	import { cn } from '$lib/utils.js';
	import type { IconType } from '$lib/icons';

	interface Props {
		title: string;
		value: string;
		label: string;
		icon: IconType;
		meterValue?: number | null;
		labelClass?: ClassValue;
	}

	let { title, value, label, icon: Icon, meterValue = null, labelClass }: Props = $props();

	const meterPercent = $derived.by(() => {
		if (meterValue === null) return 0;
		return Math.max(0, Math.min(100, meterValue));
	});

	const meterWidth = $derived.by(() => {
		return `width: ${meterPercent}%`;
	});

	const meterDotStyle = $derived.by(() => {
		return `left: calc(${meterPercent}% - 4px)`;
	});
</script>

<div class="min-w-0 px-2.5 py-2.5">
	<div class="flex items-start justify-between gap-2">
		<p class="flex items-center gap-1 text-[10px] font-semibold tracking-wide text-foreground/70 uppercase">
			<Icon class="size-3.5" />
			{title}
		</p>
		<p class="text-base font-semibold tracking-tight tabular-nums">{value}</p>
	</div>
	<p class={cn('mt-0.5 text-[11px] text-muted-foreground/90', labelClass)}>{label}</p>

	{#if meterValue !== null}
		<div class="mt-2.5">
			<div class="relative h-1.5 overflow-hidden rounded-full bg-muted/45">
				<div class="pointer-events-none absolute inset-0">
					{#each [25, 50, 75] as tick (tick)}
						<span class="absolute top-0 h-full w-px bg-foreground/15 opacity-60" style={`left: ${tick}%`}></span>
					{/each}
				</div>
				<div
					class="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-primary/65 via-primary to-violet-500/85 transition-[width] duration-700 ease-out motion-reduce:transition-none"
					style={meterWidth}
				></div>
				{#if meterPercent > 0}
					<div
						class="absolute top-1/2 size-2 -translate-y-1/2 rounded-full bg-primary shadow-[0_0_0_2px_hsl(var(--background))] transition-[left] duration-700 ease-out motion-reduce:transition-none"
						style={meterDotStyle}
					></div>
				{/if}
			</div>
		</div>
	{/if}
</div>
