<script lang="ts">
	import XIcon from '@lucide/svelte/icons/x';
	import { m } from '$lib/paraglide/messages';
	import { useEnvSelector } from './context.svelte';

	interface Props {
		onClearSavedFilter?: () => void;
	}

	let { onClearSavedFilter }: Props = $props();

	const ctx = useEnvSelector();

	// Use context methods for derived values
	const additionalSelectedTags = $derived(ctx.getAdditionalSelectedTags());
	const additionalExcludedTags = $derived(ctx.getAdditionalExcludedTags());
	const isStatusAdditional = $derived(ctx.isStatusAdditional());
	const isTagModeAdditional = $derived(ctx.isTagModeAdditional());
	const hasAdditionalFilters = $derived(
		additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0 || isStatusAdditional
	);
	const showChips = $derived(ctx.activeSavedFilter || hasAdditionalFilters);
</script>

{#snippet removableChip(label: string, onRemove: () => void, isExcluded = false)}
	<span
		class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium {isExcluded
			? 'bg-destructive/15 text-destructive'
			: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'}"
	>
		{label}
		<button class="hover:bg-background/50 -mr-0.5 rounded p-0.5" onclick={onRemove}>
			<XIcon class="size-3" />
		</button>
	</span>
{/snippet}

{#if showChips}
	<div class="flex flex-wrap items-center gap-2">
		<!-- Active saved filter badge -->
		{#if ctx.activeSavedFilter}
			<span class="bg-primary/15 text-primary inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
				{ctx.activeSavedFilter.name}
				<button class="hover:bg-background/50 -mr-0.5 rounded p-0.5" onclick={onClearSavedFilter}>
					<XIcon class="size-3" />
				</button>
			</span>
		{/if}

		<!-- Additional status filter -->
		{#if isStatusAdditional}
			<span class="bg-muted text-muted-foreground inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
				<span class="size-2 rounded-full {ctx.filters.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500'}"></span>
				{ctx.filters.statusFilter === 'online' ? m.common_online() : m.common_offline()}
				<button class="hover:bg-background/50 -mr-0.5 rounded p-0.5" onclick={ctx.clearStatus}>
					<XIcon class="size-3" />
				</button>
			</span>
		{/if}

		<!-- Tag mode badge -->
		{#if isTagModeAdditional}
			<span class="bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs font-medium">
				{ctx.filters.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
			</span>
		{/if}

		<!-- Additional selected tags -->
		{#each additionalSelectedTags as tag (tag)}
			{@render removableChip(tag, () => ctx.removeTag(tag))}
		{/each}

		<!-- Additional excluded tags -->
		{#each additionalExcludedTags as tag (tag)}
			{@render removableChip(tag, () => ctx.removeExcludedTag(tag), true)}
		{/each}

		<!-- Clear additional tags button -->
		{#if additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0}
			<button class="text-muted-foreground hover:text-foreground text-xs" onclick={ctx.resetTags}>
				{m.common_clear_tags()}
			</button>
		{/if}
	</div>
{/if}
