<script lang="ts">
	import { getSettingsFormContext, hasSettingsFormContext } from '#lib/hooks/settings-form-context.js';
	import type { Snippet } from 'svelte';
	import { UiConfigDisabledTag } from '#lib/components/badges/index.js';
	import StatCard from '#lib/components/stat-card.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ResetIcon, type IconType } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import type { SettingsPageType, SettingsStatCard } from './types.js';

	interface Props {
		title: string;
		description?: string;
		icon: IconType;
		pageType?: SettingsPageType;
		showReadOnlyTag?: boolean;
		actionButtons?: ActionButton[];
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

	const formContext = hasSettingsFormContext() ? getSettingsFormContext() : undefined;
	const formState = $derived(formContext?.activeForm);
</script>

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
					{#if description}
						<p class="mt-1 hidden text-sm text-muted-foreground sm:block sm:text-base">{@html description}</p>
					{/if}
					{#if pageType === 'management' && statCards.length > 0}
						<div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
							{#each statCards as card, i (card)}
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
							<span class="mr-2 text-xs text-warning">{m.common_unsaved_changes()}</span>
						{:else}
							<span class="mr-2 text-xs text-success">{m.common_all_changes_saved()}</span>
						{/if}

						{#if formState.hasChanges && formState.resetFunction}
							<ArcaneButton
								action="base"
								tone="outline"
								size="sm"
								onclick={() => formState?.resetFunction?.()}
								disabled={formState.isLoading}
								class="gap-2"
								icon={ResetIcon}
								customLabel={m.common_reset()}
							/>
						{/if}

						<ArcaneButton
							action="save"
							onclick={() => formState?.saveFunction?.()}
							disabled={!formState.hasChanges}
							loading={formState.isLoading}
							size="sm"
							class="min-w-20 gap-2"
						/>
					</div>
				{/if}

				{#if pageType === 'management' && actionButtons.length > 0}
					<ActionButtonGroup buttons={actionButtons} />
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
