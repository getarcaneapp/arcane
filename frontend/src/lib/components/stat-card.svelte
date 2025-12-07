<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { PhosphorIcon } from '$lib/types/icon.type';
	import type { ClassValue } from 'svelte/elements';

	interface Props {
		title: string;
		value: string | number;
		icon: PhosphorIcon;
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
		'bg-card/90 group hover-lift relative overflow-hidden rounded-xl border p-5 shadow-sm backdrop-blur-sm transition-all duration-300',
		iconColor,
		className
	)}
	style="--stat-hover-tint: currentColor;"
>
	<div
		class="pointer-events-none absolute inset-0 rounded-xl bg-gradient-to-br from-[var(--stat-hover-tint)]/10 to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100"
	></div>

	<div
		class="pointer-events-none absolute -top-8 -right-8 size-24 rounded-full bg-[var(--stat-hover-tint)]/20 opacity-0 blur-2xl transition-opacity duration-500 group-hover:opacity-100"
	></div>

	<div class="relative flex items-center justify-between gap-4">
		<div class="flex-1 space-y-1.5">
			<p class="text-muted-foreground text-xs font-medium tracking-wider uppercase opacity-80">
				{title}
			</p>
			<p class="text-foreground text-2xl font-bold tabular-nums transition-transform duration-300">
				{value}
			</p>
			{#if subtitle}
				<p class="text-muted-foreground text-xs opacity-70">{subtitle}</p>
			{/if}
		</div>

		<div
			class={cn(
				'bg-card/80 relative shrink-0 rounded-xl border border-white/10 p-3 shadow-lg backdrop-blur-sm transition-all duration-300 group-hover:bg-[var(--stat-hover-tint)]/10',
				'group-hover:-translate-y-0.5 group-hover:shadow-xl',
				bgColor
			)}
		>
			<div class="pointer-events-none absolute inset-0 rounded-xl bg-linear-to-br from-white/20 to-transparent opacity-50"></div>
			<Icon class={cn('relative size-5 transition-transform duration-300', iconColor)} />
		</div>
	</div>
</div>
