<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import StatCard from '$lib/components/stat-card.svelte';
	import type { Snippet } from 'svelte';
	import type { Action } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon, type IconType } from '$lib/icons';

	export interface ActionButton {
		id: string;
		action: Action;
		label: string;
		loadingLabel?: string;
		loading?: boolean;
		disabled?: boolean;
		onclick: () => void;
		showOnMobile?: boolean;
	}

	export interface StatCardConfig {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
	}

	export type StatCardsColumns = 'auto' | 1 | 2 | 3 | 4 | 5;

	interface Props {
		title: string;
		subtitle?: string;
		actionButtons?: ActionButton[];
		statCards?: StatCardConfig[];
		statCardsColumns?: StatCardsColumns;
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
		containerClass?: string;
	}

	let {
		title,
		subtitle,
		actionButtons = [],
		statCards = [],
		statCardsColumns = 'auto',
		mainContent,
		additionalContent,
		class: className = '',
		containerClass = 'space-y-8 pb-5 md:space-y-10 md:pb-5'
	}: Props = $props();

	const mobileVisibleButtons = $derived(actionButtons.filter((btn) => btn.showOnMobile));
	const mobileDropdownButtons = $derived(actionButtons.filter((btn) => !btn.showOnMobile));

	const getStatCardsGridClass = (columns: typeof statCardsColumns) => {
		switch (columns) {
			case 1:
				return 'grid-cols-1';
			case 2:
				return 'grid-cols-2 sm:grid-cols-2';
			case 3:
				return 'grid-cols-2 sm:grid-cols-3 [&>*:nth-child(3)]:col-span-2 sm:[&>*:nth-child(3)]:col-span-1';
			case 4:
				return 'grid-cols-2 sm:grid-cols-2 lg:grid-cols-4';
			case 5:
				return 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-5';
			case 'auto':
			default:
				if (statCards.length <= 2) return 'grid-cols-2 sm:grid-cols-2';
				if (statCards.length === 3)
					return 'grid-cols-2 sm:grid-cols-3 [&>*:nth-child(3)]:col-span-2 sm:[&>*:nth-child(3)]:col-span-1';
				if (statCards.length === 4) return 'grid-cols-2 sm:grid-cols-2 lg:grid-cols-4';
				return 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-5';
		}
	};
</script>

<div class="{containerClass} {className}">
	<div class="relative flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
		<div>
			<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
			{#if subtitle}
				<p class="text-muted-foreground mt-1 text-sm">{subtitle}</p>
			{/if}
		</div>

		{#if actionButtons.length > 0}
			<div class="hidden items-center gap-2 sm:flex">
				{#each actionButtons as button}
					<ArcaneButton
						action={button.action}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						onclick={button.onclick}
					/>
				{/each}
			</div>

			<div class="absolute top-4 right-4 flex items-center gap-2 sm:hidden">
				{#each mobileVisibleButtons as button}
					<ArcaneButton
						action={button.action}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						onclick={button.onclick}
						size="sm"
					/>
				{/each}

				{#if mobileDropdownButtons.length > 0}
					<DropdownMenu.Root>
						<DropdownMenu.Trigger class="bg-background/70 inline-flex size-9 items-center justify-center rounded-lg border">
							<span class="sr-only">Open menu</span>
							<EllipsisIcon class="size-4" />
						</DropdownMenu.Trigger>

						<DropdownMenu.Content
							align="end"
							class="bg-popover/90 z-50 min-w-[160px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
						>
							<DropdownMenu.Group>
								{#each mobileDropdownButtons as button}
									<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
										{button.loading ? button.loadingLabel || button.label : button.label}
									</DropdownMenu.Item>
								{/each}
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				{/if}
			</div>
		{/if}
	</div>

	{#if statCards && statCards.length > 0}
		<div class="grid gap-4 {getStatCardsGridClass(statCardsColumns)}">
			{#each statCards as card}
				<StatCard
					title={card.title}
					value={card.value}
					subtitle={card.subtitle}
					icon={card.icon}
					iconColor={card.iconColor}
					bgColor={card.bgColor}
					class={card.class}
				/>
			{/each}
		</div>
	{/if}

	{@render mainContent()}

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>
