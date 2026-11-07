<script lang="ts">
	import type { Snippet } from 'svelte';

	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { SettingLock } from '#lib/components/badges/index.js';
	import StatCard from '#lib/components/stat-card.svelte';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { getSettingsFormContext, hasSettingsFormContext } from '#lib/hooks/settings-form-context.js';
	import { ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import type { SettingsPageType, SettingsStatCard } from './types.js';

	interface Props {
		title: string;
		description?: string | Snippet;
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
	const isMobile = new IsMobile();
</script>

<div class={cn('pt-3 pb-5 sm:pb-10 md:pt-5', className)}>
	<div>
		<div class="flex flex-wrap items-start justify-between gap-4">
			<div class="min-w-0 flex-1">
				<h1 class="font-heading text-2xl sm:text-3xl">{title}</h1>
				{#if description}
					<p class="mt-1 hidden text-sm text-muted-foreground sm:block">
						{#if typeof description === 'function'}{@render description()}{:else}{description}{/if}
					</p>
				{/if}
				{#if pageType === 'management' && statCards.length > 0}
					<div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
						{#each statCards as card, i (i)}
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

			<div class="flex shrink-0 items-center justify-end gap-2">
				{#if showReadOnlyTag}
					<SettingLock />
				{/if}

				{#if pageType === 'form' && formState?.saveFunction && !showReadOnlyTag && !isMobile.current}
					<div class="flex flex-wrap items-center justify-end gap-2">
						{#if formState.hasChanges}
							<span class="mr-2 text-xs text-warning-foreground">{m.common_unsaved_changes()}</span>
						{:else if formState.hasSaved}
							<span class="mr-2 text-xs text-mint-foreground">{m.common_all_changes_saved()}</span>
						{/if}

						{#if formState.hasChanges && formState.resetFunction}
							<ArcaneButton
								action="base"
								tone="outline"
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

	<div class="mt-5 md:mt-7">
		{@render mainContent()}
	</div>

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>
