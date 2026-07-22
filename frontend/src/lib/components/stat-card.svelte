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
		<span class="text-[11px] leading-none font-medium tracking-[0.08em] whitespace-nowrap text-muted-foreground uppercase">
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
				'pressable -mx-0.5 flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 transition-colors hover:bg-foreground/5 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none',
				active && 'bg-primary/10 ring-1 ring-primary/30',
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
			'hover-lift group relative overflow-hidden rounded-xl border border-border/70 bg-card/60 p-4 backdrop-blur-md transition-colors',
			iconColor,
			className
		)}
	>
		<div class="relative flex items-start justify-between gap-3">
			<div class="space-y-2">
				<p class="text-sm font-medium tracking-wide text-muted-foreground">
					{title}
				</p>
				<h3 class="text-2xl font-semibold tracking-tight tabular-nums">
					{value}
				</h3>
				{#if subtitle}
					<p class="text-xs text-muted-foreground">{subtitle}</p>
				{/if}
			</div>

			<div
				class={cn(
					'flex size-9 shrink-0 items-center justify-center rounded-lg ring-1 ring-foreground/5 transition-transform duration-200 ring-inset group-hover:scale-105',
					bgColor
				)}
			>
				<Icon class={cn('size-5', iconColor)} />
			</div>
		</div>
	</div>
{/if}
