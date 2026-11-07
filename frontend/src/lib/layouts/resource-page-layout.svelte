<script lang="ts" module>
	import type { ActionButton as ActionButtonType } from '#lib/components/action-button-group/index.js';
	export type ActionButton = ActionButtonType;

	export interface StatCardConfig {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: import('#lib/icons/index.js').IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
		onclick?: () => void;
		active?: boolean;
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	import { ActionButtonGroup } from '#lib/components/action-button-group/index.js';
	import StatCard from '#lib/components/stat-card.svelte';
	import { cn } from '#lib/utils.js';

	interface Props {
		title: string;
		subtitle?: string;
		actionButtons?: ActionButton[];
		statCards?: StatCardConfig[];
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
	}

	let {
		title,
		subtitle,
		actionButtons = [],
		statCards = [],
		mainContent,
		additionalContent,
		class: className = ''
	}: Props = $props();
</script>

<div class={cn('space-y-5 md:space-y-7', className)}>
	<header class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
		<div class="min-w-0 flex-1">
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

		<div class="flex w-full min-w-0 items-center justify-start gap-2 sm:w-auto sm:justify-end">
			<ActionButtonGroup buttons={actionButtons} />
		</div>
	</header>

	<div>
		{@render mainContent()}

		{#if additionalContent}
			{@render additionalContent()}
		{/if}
	</div>
</div>
