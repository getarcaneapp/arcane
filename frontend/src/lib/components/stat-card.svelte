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
		variant?: 'default' | 'mini';
		/** When provided (mini variant), the card becomes a button — e.g. to apply a table filter. */
		onclick?: () => void;
		/** Highlights the mini card as the currently-applied filter. */
		active?: boolean;
	}

	let {
		title,
		value,
		icon: Icon,
		iconColor = 'text-primary',
		bgColor = 'bg-primary/10',
		subtitle,
		class: className,
		variant = 'default',
		onclick,
		active = false
	}: Props = $props();
</script>

{#snippet miniContent()}
	<Icon class={cn('size-3.5 opacity-80', iconColor)} />
	<div class="flex items-baseline gap-1">
		<span class="text-sm leading-none font-semibold tabular-nums">
			{value}
		</span>
		<span class="text-muted-foreground text-[11px] leading-none font-medium tracking-[0.08em] whitespace-nowrap uppercase">
			{title}
		</span>
	</div>
{/snippet}

{#if variant === 'mini'}
	{#if onclick}
		<button
			type="button"
			{onclick}
			aria-pressed={active}
			class={cn(
				'hover:bg-foreground/5 focus-visible:ring-primary/40 -mx-0.5 flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 transition-colors focus-visible:ring-2 focus-visible:outline-none',
				active && 'bg-primary/10 ring-primary/30 ring-1',
				className
			)}
		>
			{@render miniContent()}
		</button>
	{:else}
		<div class={cn('flex items-center gap-1.5 px-1', className)}>
			{@render miniContent()}
		</div>
	{/if}
{:else}
	<div
		class={cn(
			'bg-card/60 backdrop-blur-md border-border/70 group relative overflow-hidden rounded-xl border p-4 transition-colors',
			iconColor,
			className
		)}
	>
		<div class="relative flex items-start justify-between">
			<div class="space-y-2">
				<p class="text-muted-foreground text-sm font-medium tracking-wide">
					{title}
				</p>
				<h3 class="text-2xl font-semibold tracking-tight tabular-nums">
					{value}
				</h3>
				{#if subtitle}
					<p class="text-muted-foreground text-xs">{subtitle}</p>
				{/if}
			</div>

			<div class={cn('flex size-9 items-center justify-center rounded-md transition-colors', bgColor)}>
				<Icon class={cn('size-5', iconColor)} />
			</div>
		</div>
	</div>
{/if}
