<script lang="ts">
	import type { Snippet } from 'svelte';
	import { UiConfigDisabledTag } from '$lib/components/badges/index.js';
	import StatCard from '$lib/components/stat-card.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { Action } from '$lib/components/arcane-button/index.js';
	import { getContext } from 'svelte';
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

	interface Props {
		title: string;
		description?: string;
		icon: IconType;
		pageType?: SettingsPageType;
		showReadOnlyTag?: boolean;
		actionButtons?: SettingsActionButton[];
		statCards?: SettingsStatCard[];
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
					<div class="flex-1">
						<h1 class="settings-title min-w-0 text-xl sm:text-3xl">{title}</h1>
					</div>

					{#if pageType === 'management' && statCards && statCards.length > 0}
						<div class="hidden flex-1 items-center justify-center md:flex">
							<div class="border-border/50 relative overflow-hidden rounded-full border">
								<!-- Subtle muted background overlay -->
								<div class="bg-muted/50 absolute inset-0"></div>

								<!-- Glass effect container -->
								<div class="relative flex items-center gap-4 px-4 py-1.5 backdrop-blur-md">
									{#each statCards as card, i}
										{#if i > 0}
											<div class="bg-border/50 h-4 w-px"></div>
										{/if}
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

					<div class="flex flex-1 shrink-0 items-center justify-end gap-2">
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
									<ArcaneButton
										action="base"
										tone="outline"
										size="sm"
										onclick={() => formState.resetFunction && formState.resetFunction()}
										disabled={formState.isLoading}
										class="gap-2"
										icon={ResetIcon}
										customLabel={m.common_reset()}
									/>
								{/if}

								<ArcaneButton
									action="save"
									onclick={async () => {
										if (formState.saveFunction) {
											await formState.saveFunction();
										}
									}}
									disabled={!formState.hasChanges || !formState.saveFunction}
									loading={formState.isLoading}
									size="sm"
									class="min-w-[80px] gap-2"
								/>
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
										<DropdownMenu.Trigger>
											{#snippet child({ props })}
												<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="bg-background/70 size-8 border">
													<span class="sr-only">Open menu</span>
													<EllipsisIcon class="size-4" />
												</ArcaneButton>
											{/snippet}
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

	<div class="mt-6 sm:mt-8">
		{@render mainContent()}
	</div>

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>
