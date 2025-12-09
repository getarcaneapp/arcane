<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { ClassValue } from 'svelte/elements';
	import { type IconType } from '$lib/icons';

	interface Props {
		title: string;
		value: string | number;
		icon: IconType;
		iconColor?: string;
		bgColor?: string;
		subtitle?: string;
		class?: ClassValue;
	}

	let {
		title,
		value,
		icon: Icon,
		iconColor = 'text-primary',
		bgColor = 'bg-primary/10',
		subtitle,
		class: className
	}: Props = $props();
</script>

<div
	class={cn(
		'bg-card group relative overflow-hidden rounded-2xl border p-6 transition-all duration-300 hover:-translate-y-1 hover:shadow-lg',
		iconColor,
		className
	)}
	style="--stat-hover-tint: currentColor;"
>
	<div
		class="pointer-events-none absolute inset-0 bg-gradient-to-br from-[var(--stat-hover-tint)]/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
	></div>

	<div class="relative flex items-start justify-between">
		<div class="space-y-2">
			<p class="text-muted-foreground text-sm font-medium tracking-wide">
				{title}
			</p>
			<h3 class="text-3xl font-bold tracking-tight tabular-nums">
				{value}
			</h3>
			{#if subtitle}
				<p class="text-muted-foreground text-xs">{subtitle}</p>
			{/if}
		</div>

		<div
			class={cn(
				'flex size-10 items-center justify-center rounded-full transition-colors duration-300',
				'bg-transparent group-hover:bg-[var(--stat-hover-tint)]/10'
			)}
		>
			<Icon class={cn('size-6 transition-transform duration-300 group-hover:scale-110', iconColor)} />
		</div>
	</div>
</div>
