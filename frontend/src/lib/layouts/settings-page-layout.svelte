<script lang="ts">
	import type { Snippet } from 'svelte';
	import { UiConfigDisabledTag } from '$lib/components/badges/index.js';
	import StatCard from '$lib/components/stat-card.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { Action } from '$lib/components/arcane-button/index.js';
	import { getContext } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages';
	import { EllipsisIcon, ResetIcon, SaveIcon, type IconType } from '$lib/icons';

	export interface SettingsActionButton {
		id: string;
		action: Action;
		label: string;
		loadingLabel?: string;
		loading?: boolean;
		disabled?: boolean;
		onclick: () => void;
		showOnMobile?: boolean;
	}

	export interface SettingsStatCard {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
	}

	export type SettingsPageType = 'form' | 'management';
	export type StatCardsColumns = 'auto' | 1 | 2 | 3 | 4 | 5;

	interface Props {
		title: string;
		description?: string;
		icon: IconType;
		pageType?: SettingsPageType;
		showReadOnlyTag?: boolean;
		actionButtons?: SettingsActionButton[];
		statCards?: SettingsStatCard[];
		statCardsColumns?: StatCardsColumns;
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
	}

	let {
		title,
		description,
		icon: Icon,
		pageType = 'form',
		showReadOnlyTag = false,
		actionButtons = [],
		statCards = [],
		statCardsColumns = 'auto',
		mainContent,
		additionalContent,
		class: className = ''
	}: Props = $props();

	const mobileVisibleButtons = $derived(actionButtons.filter((btn) => btn.showOnMobile));
	const mobileDropdownButtons = $derived(actionButtons.filter((btn) => !btn.showOnMobile));

	// Get form state from context if available
	const formState = getContext<{
		hasChanges: boolean;
		isLoading: boolean;
		saveFunction: (() => Promise<void>) | null;
		resetFunction: (() => void) | null;
	}>('settingsFormState');

	const getStatCardsGridClass = (columns: typeof statCardsColumns) => {
		switch (columns) {
			case 1:
				return 'grid-cols-1';
			case 2:
				return 'grid-cols-1 sm:grid-cols-2';
			case 3:
				return 'grid-cols-1 sm:grid-cols-3';
			case 4:
				return 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-4';
			case 5:
				return 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-5';
			case 'auto':
			default:
				if (statCards.length <= 2) return 'grid-cols-1 sm:grid-cols-2';
				if (statCards.length === 3) return 'grid-cols-1 sm:grid-cols-3';
				if (statCards.length === 4) return 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-4';
				return 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-5';
		}
	};
</script>

<div class="px-2 py-4 pb-5 sm:px-6 sm:py-6 sm:pb-10 lg:px-8 {className}">
	<div
		class="from-background/60 via-background/40 to-background/60 relative overflow-hidden rounded-xl border bg-gradient-to-br p-4 shadow-sm sm:p-6"
	>
		<div class="bg-primary/10 pointer-events-none absolute -top-10 -right-10 size-40 rounded-full blur-3xl"></div>
		<div class="bg-muted/40 pointer-events-none absolute -bottom-10 -left-10 size-40 rounded-full blur-3xl"></div>
		<div class="relative flex items-start gap-3 sm:gap-4">
			<div
				class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
			>
				<Icon class="size-4 sm:size-5" />
			</div>

			<div class="min-w-0 flex-1">
				<div class="flex items-start justify-between gap-3">
					<h1 class="settings-title min-w-0 text-xl sm:text-3xl">{title}</h1>
					<div class="flex shrink-0 items-center gap-2">
						{#if showReadOnlyTag}
							<UiConfigDisabledTag />
						{/if}

						{#if pageType === 'form' && formState && !showReadOnlyTag}
							<div class="hidden items-center gap-2 sm:flex">
								{#if formState.hasChanges}
									<span class="mr-2 text-xs text-orange-600 dark:text-orange-400"> Unsaved changes </span>
								{:else if !formState.hasChanges && formState.saveFunction}
									<span class="mr-2 text-xs text-green-600 dark:text-green-400"> All changes saved </span>
								{/if}

								{#if formState.hasChanges && formState.resetFunction}
									<Button
										variant="outline"
										size="sm"
										onclick={() => formState.resetFunction && formState.resetFunction()}
										disabled={formState.isLoading}
										class="gap-2"
									>
										<ResetIcon class="size-4" />
										<span class="hidden sm:inline">{m.common_reset()}</span>
									</Button>
								{/if}

								<Button
									onclick={async () => {
										if (formState.saveFunction) {
											await formState.saveFunction();
										}
									}}
									disabled={formState.isLoading || !formState.hasChanges || !formState.saveFunction}
									size="sm"
									class="min-w-[80px] gap-2"
								>
									{#if formState.isLoading}
										<div class="border-background size-4 animate-spin rounded-full border-2 border-t-transparent"></div>
										<span class="hidden sm:inline">{m.common_saving()}</span>
									{:else}
										<SaveIcon class="size-4" />
										<span class="hidden sm:inline">{m.common_save()}</span>
									{/if}
								</Button>
							</div>
						{/if}

						{#if pageType === 'management' && actionButtons.length > 0}
							<div class="hidden items-center gap-2 sm:flex">
								{#each actionButtons as button}
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
							</div>

							<div class="flex items-center gap-2 sm:hidden">
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
										<DropdownMenu.Trigger
											class="bg-background/70 inline-flex size-8 items-center justify-center rounded-lg border"
										>
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
				</div>
				{#if description}
					<p class="text-muted-foreground mt-1 text-sm sm:text-base">{@html description}</p>
				{/if}
			</div>
		</div>
	</div>

	{#if pageType === 'management' && statCards && statCards.length > 0}
		<div class="mt-6 grid gap-4 sm:mt-8 {getStatCardsGridClass(statCardsColumns)}">
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

	<div class="mt-6 sm:mt-8">
		{@render mainContent()}
	</div>

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>
