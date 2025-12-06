<script lang="ts">
	import XIcon from '@lucide/svelte/icons/x';
	import { m } from '$lib/paraglide/messages';
	import { useEnvSelector } from './context.svelte';

	interface Props {
		onClearSavedFilter?: () => void;
	}

	let { onClearSavedFilter }: Props = $props();

	const ctx = useEnvSelector();

	// Compute which tags/filters are "additional" (not part of active saved filter)
	const savedFilter = $derived(ctx.activeSavedFilter);
	const additionalSelectedTags = $derived(
		savedFilter ? ctx.filters.selectedTags.filter((t) => !savedFilter.selectedTags.includes(t)) : ctx.filters.selectedTags
	);
	const additionalExcludedTags = $derived(
		savedFilter ? ctx.filters.excludedTags.filter((t) => !savedFilter.excludedTags.includes(t)) : ctx.filters.excludedTags
	);
	const isStatusAdditional = $derived(
		savedFilter ? ctx.filters.statusFilter !== savedFilter.statusFilter : ctx.filters.statusFilter !== 'all'
	);
	const isTagModeAdditional = $derived(
		ctx.filters.selectedTags.length > 1 &&
			(savedFilter ? ctx.filters.tagMode !== savedFilter.tagMode : ctx.filters.tagMode !== 'any')
	);
	const hasAdditionalFilters = $derived(
		additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0 || isStatusAdditional
	);
	const showChips = $derived(savedFilter || hasAdditionalFilters);

	function resetTags() {
		ctx.updateFilters(
			savedFilter
				? { selectedTags: [...savedFilter.selectedTags], excludedTags: [...savedFilter.excludedTags] }
				: { selectedTags: [], excludedTags: [] }
		);
	}
</script>

{#snippet chip(label: string, onRemove: () => void, variant: 'include' | 'exclude' | 'neutral' = 'neutral')}
	{@const colors = {
		include: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400',
		exclude: 'bg-destructive/15 text-destructive',
		neutral: 'bg-muted text-muted-foreground'
	}}
	<span class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium {colors[variant]}">
		{label}
		<button class="hover:bg-background/50 -mr-0.5 rounded p-0.5" onclick={onRemove}>
			<XIcon class="size-3" />
		</button>
	</span>
{/snippet}

{#if showChips}
	<div class="flex flex-wrap items-center gap-2">
		<!-- Active saved filter -->
		{#if savedFilter}
			<span class="bg-primary/15 text-primary inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
				{savedFilter.name}
				<button class="hover:bg-background/50 -mr-0.5 rounded p-0.5" onclick={onClearSavedFilter}>
					<XIcon class="size-3" />
				</button>
			</span>
		{/if}

		<!-- Status filter -->
		{#if isStatusAdditional}
			<span class="bg-muted text-muted-foreground inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
				<span class="size-2 rounded-full {ctx.filters.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500'}"></span>
				{ctx.filters.statusFilter === 'online' ? m.common_online() : m.common_offline()}
				<button
					class="hover:bg-background/50 -mr-0.5 rounded p-0.5"
					onclick={() => ctx.setStatus(savedFilter?.statusFilter ?? 'all')}
				>
					<XIcon class="size-3" />
				</button>
			</span>
		{/if}

		<!-- Tag mode -->
		{#if isTagModeAdditional}
			<span class="bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs font-medium">
				{ctx.filters.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
			</span>
		{/if}

		<!-- Selected tags -->
		{#each additionalSelectedTags as tag (tag)}
			{@render chip(tag, () => ctx.removeTag(tag), 'include')}
		{/each}

		<!-- Excluded tags -->
		{#each additionalExcludedTags as tag (tag)}
			{@render chip(tag, () => ctx.removeExcludedTag(tag), 'exclude')}
		{/each}

		<!-- Clear tags button -->
		{#if additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0}
			<button class="text-muted-foreground hover:text-foreground text-xs" onclick={resetTags}>
				{m.common_clear_tags()}
			</button>
		{/if}
	</div>
{/if}
