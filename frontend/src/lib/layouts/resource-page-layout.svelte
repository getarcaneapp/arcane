<script lang="ts">
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import HeaderCard from '$lib/components/header-card.svelte';
	import StatCard from '$lib/components/stat-card.svelte';
	import type { Snippet } from 'svelte';
	import type { IconType } from '$lib/icons';

	export type { ActionButton };

	export interface StatCardConfig {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
	}

	interface Props {
		title: string;
		subtitle?: string;
		icon?: IconType;
		actionButtons?: ActionButton[];
		statCards?: StatCardConfig[];
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
		containerClass?: string;
	}

	let {
		title,
		subtitle,
		icon: Icon,
		actionButtons = [],
		statCards = [],
		mainContent,
		additionalContent,
		class: className = '',
		containerClass = 'space-y-8 pb-5 md:space-y-10 md:pb-5'
	}: Props = $props();
</script>

<div class="{containerClass} {className}">
	<HeaderCard>
		<div class="flex items-center justify-between gap-4">
			<div class="flex min-w-64 flex-1 items-center gap-3 sm:gap-4">
				{#if Icon}
					<div
						class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
					>
						<Icon class="size-4 sm:size-5" />
					</div>
				{/if}
				<div class="min-w-0">
					<h1 class="text-3xl font-bold tracking-tight">{title}</h1>
					{#if subtitle}
						<p class="text-muted-foreground mt-1 text-sm sm:text-base">{subtitle}</p>
					{/if}
				</div>
			</div>

			{#if statCards && statCards.length > 0}
				<div class="hidden shrink items-center justify-center pr-8 md:flex">
					<div class="border-border/50 bg-muted/30 relative overflow-hidden rounded-xl border backdrop-blur-sm">
						<div class="flex flex-wrap items-center justify-center gap-x-4 gap-y-2 px-4 py-2">
							{#each statCards as card, i}
								<StatCard
									variant="mini"
									title={card.title}
									value={card.value}
									icon={card.icon}
									iconColor={card.iconColor}
									class={card.class}
								/>
							{/each}
						</div>
					</div>
				</div>
			{/if}

			<ActionButtonGroup buttons={actionButtons} />
		</div>
	</HeaderCard>

	{@render mainContent()}

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>
