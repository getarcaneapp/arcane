<script lang="ts" module>
	import type { ActionButton as ActionButtonType } from '$lib/components/action-button-group/index.js';
	export type ActionButton = ActionButtonType;

	export interface StatCardConfig {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: import('$lib/icons').IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
		/** When provided, the mini stat card becomes a clickable filter trigger. */
		onclick?: () => void;
		/** Highlights the mini stat card as the currently-applied filter. */
		active?: boolean;
	}
</script>

<script lang="ts">
	import { ActionButtonGroup } from '$lib/components/action-button-group/index.js';
	import StatCard from '$lib/components/stat-card.svelte';
	import type { Snippet } from 'svelte';
	import type { IconType } from '$lib/icons';
	import { cn } from '$lib/utils';

	interface Props {
		title: string;
		subtitle?: string;
		icon?: IconType;
		actionButtons?: ActionButton[];
		statCards?: StatCardConfig[];
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
	}

	let {
		title,
		subtitle,
		icon: Icon,
		actionButtons = [],
		statCards = [],
		mainContent,
		additionalContent,
		class: className = ''
	}: Props = $props();
</script>

<div class={cn('space-y-5 pt-3 md:space-y-7 md:pt-5', className)}>
	<header class="flex items-start justify-between gap-4">
		<div class="flex min-w-0 flex-1 items-start gap-3 sm:gap-4">
			{#if Icon}
				<div
					class="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary shadow-xs ring-1 ring-primary/15 ring-inset sm:size-10"
				>
					<Icon class="size-4.5 sm:size-5" />
				</div>
			{/if}
			<div class="min-w-0">
				<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
				{#if subtitle}
					<p class="mt-1 text-sm text-muted-foreground">{subtitle}</p>
				{/if}
				{#if statCards && statCards.length > 0}
					<div class="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1">
						{#each statCards as card, i (card.title ?? i)}
							<StatCard
								variant="mini"
								title={card.title}
								value={card.value}
								icon={card.icon}
								iconColor={card.iconColor}
								class={card.class}
								onclick={card.onclick}
								active={card.active}
							/>
							{#if i < statCards.length - 1}
								<span class="hidden h-4 w-px bg-border/60 sm:block" aria-hidden="true"></span>
							{/if}
						{/each}
					</div>
				{/if}
			</div>
		</div>

		<ActionButtonGroup buttons={actionButtons} />
	</header>

	<div class="md:-mx-5 md:-mb-5 md:px-2 md:pb-2">
		{@render mainContent()}

		{#if additionalContent}
			{@render additionalContent()}
		{/if}
	</div>
</div>
