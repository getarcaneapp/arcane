<script lang="ts">
	import type { Snippet } from 'svelte';
	import { UiConfigDisabledTag } from '#lib/components/badges/index.js';
	import StatCard from '#lib/components/stat-card.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { getContext } from 'svelte';
	import { m } from '#lib/paraglide/messages';
	import { EllipsisIcon, ResetIcon, type IconType, ArrowDownIcon } from '#lib/icons';
	import { page } from '$app/state';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { getRouteAccessSurfaces, pathMatchesAccessSurface } from '#lib/utils/access-policy';
	import { cn } from '#lib/utils';
	import type { SettingsActionButton, SettingsPageType, SettingsStatCard } from './types.js';

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

	const scopeMode = $derived(
		getRouteAccessSurfaces(page.data['permissionsManifest']).find((surface) =>
			pathMatchesAccessSurface(page.url.pathname, surface)
		)?.scopeMode
	);

	const mobileVisibleButtons = $derived(actionButtons.filter((btn) => btn.primary || btn.showOnMobile));
	const mobileDropdownButtons = $derived(actionButtons.filter((btn) => !(btn.primary || btn.showOnMobile)));

	const formState = getContext<{
		hasChanges: boolean;
		isLoading: boolean;
		saveFunction: (() => Promise<void>) | null;
		resetFunction: (() => void) | null;
	}>('settingsFormState');
</script>

{#snippet ActionButtonList(buttons: SettingsActionButton[])}
	{#each buttons as button (button.id)}
		{#if button.options?.length}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<ArcaneButton
							{...props}
							action={button.action}
							icon={button.icon}
							customLabel={button.label}
							disabled={button.disabled}
							size={button.size ?? 'default'}
						>
							<ArrowDownIcon class="size-3.5 opacity-60" />
						</ArcaneButton>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content
					align="end"
					class="z-[var(--arcane-z-surface)] min-w-[160px] rounded-xl border bg-popover/90 p-1 shadow-lg backdrop-blur-md"
				>
					<DropdownMenu.Group>
						{#each button.options as option (option.label)}
							<DropdownMenu.Item onclick={option.onclick} disabled={option.disabled}>
								{#if option.icon}
									{@const OptionIcon = option.icon}
									<OptionIcon class="size-4" />
								{/if}
								{option.label}
							</DropdownMenu.Item>
						{/each}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{:else}
			<ArcaneButton
				action={button.action}
				icon={button.icon}
				customLabel={button.label}
				loadingLabel={button.loadingLabel}
				loading={button.loading}
				disabled={button.disabled}
				onclick={button.onclick}
				size={button.size ?? 'default'}
			/>
		{/if}
	{/each}
{/snippet}

<div class={cn('px-2 py-4 pb-5 sm:px-6 sm:py-6 sm:pb-10 lg:px-8', className)}>
	<div class="border-b border-border/50 pb-4 sm:pb-6">
		<div class="flex items-center justify-between gap-4">
			<div class="flex flex-1 items-start gap-3 sm:gap-4">
				{#if Icon}
					<div
						class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 sm:size-10"
					>
						<Icon class="size-4 sm:size-5" />
					</div>
				{/if}
				<div class="min-w-0">
					<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
					{#if scopeMode === 'global-only'}
						<p class="text-xs text-muted-foreground">{m.global()}</p>
					{:else if scopeMode === 'selected-env-plus-global' && environmentStore.selected}
						<p class="text-xs text-muted-foreground">{m.resource_environment_cap()}: {environmentStore.selected.name}</p>
					{/if}
					{#if description}
						<p class="mt-1 hidden text-sm text-muted-foreground sm:block sm:text-base">{@html description}</p>
					{/if}
					{#if pageType === 'management' && statCards && statCards.length > 0}
						<div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
							{#each statCards as card, i (card.title)}
								{#if i > 0}
									<div class="h-4 w-px bg-border/50"></div>
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
					{/if}
				</div>
			</div>

			<div class="flex flex-1 items-center justify-end gap-2">
				{#if showReadOnlyTag}
					<UiConfigDisabledTag />
				{/if}

				{#if pageType === 'form' && formState?.saveFunction && !showReadOnlyTag}
					<div class="hidden items-center gap-2 sm:flex">
						{#if formState.hasChanges}
							<span class="mr-2 text-xs text-orange-600 dark:text-orange-400">{m.common_unsaved_changes()}</span>
						{:else if !formState.hasChanges}
							<span class="mr-2 text-xs text-green-600 dark:text-green-400">{m.common_all_changes_saved()}</span>
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
						{@render ActionButtonList(actionButtons)}
					</div>

					<div class="flex items-center gap-2 sm:hidden">
						{@render ActionButtonList(mobileVisibleButtons)}

						{#if mobileDropdownButtons.length > 0}
							<DropdownMenu.Root>
								<DropdownMenu.Trigger>
									{#snippet child({ props })}
										<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8 border bg-background/70">
											<span class="sr-only">{m.common_open_menu()}</span>
											<EllipsisIcon class="size-4" />
										</ArcaneButton>
									{/snippet}
								</DropdownMenu.Trigger>

								<DropdownMenu.Content
									align="end"
									class="z-[var(--arcane-z-surface)] min-w-[160px] rounded-xl border bg-popover/90 p-1 shadow-lg backdrop-blur-md"
								>
									<DropdownMenu.Group>
										{#each mobileDropdownButtons as button (button.id)}
											{#if button.options?.length}
												<DropdownMenu.Label>{button.label}</DropdownMenu.Label>
												{#each button.options as option (option.label)}
													<DropdownMenu.Item onclick={option.onclick} disabled={button.disabled || option.disabled}>
														{option.label}
													</DropdownMenu.Item>
												{/each}
											{:else}
												<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
													{button.loading ? button.loadingLabel || button.label : button.label}
												</DropdownMenu.Item>
											{/if}
										{/each}
									</DropdownMenu.Group>
								</DropdownMenu.Content>
							</DropdownMenu.Root>
						{/if}
					</div>
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
