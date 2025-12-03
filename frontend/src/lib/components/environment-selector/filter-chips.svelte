<script lang="ts">
	import { Badge } from '$lib/components/ui/badge/index.js';
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

{#snippet removableChip(label: string, onRemove: () => void, colorClass?: string)}
	<Badge variant="outline" class="gap-1 pr-1 {colorClass ?? ''}">
		{label}
		<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={onRemove}>
			<XIcon class="size-3" />
		</button>
	</Badge>
{/snippet}

{#if showChips}
	<div class="flex flex-wrap items-center gap-1.5">
		<!-- Active saved filter badge -->
		{#if ctx.activeSavedFilter}
			<Badge variant="secondary" class="gap-1 pr-1">
				{ctx.activeSavedFilter.name}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={onClearSavedFilter}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/if}

		<!-- Additional status filter -->
		{#if isStatusAdditional}
			<Badge variant="outline" class="gap-1 pr-1">
				<span class="size-1.5 rounded-full {ctx.filters.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500'}"></span>
				{ctx.filters.statusFilter === 'online' ? m.common_online() : m.common_offline()}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={ctx.clearStatus}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/if}

		<!-- Tag mode badge -->
		{#if isTagModeAdditional}
			<Badge variant="outline" class="text-xs">
				{ctx.filters.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
			</Badge>
		{/if}

		<!-- Additional selected tags -->
		{#each additionalSelectedTags as tag (tag)}
			{@render removableChip(tag, () => ctx.removeTag(tag), 'text-sky-700 dark:text-sky-300')}
		{/each}

		<!-- Additional excluded tags -->
		{#each additionalExcludedTags as tag (tag)}
			{@render removableChip(tag, () => ctx.removeExcludedTag(tag), 'text-orange-700 dark:text-orange-300')}
		{/each}

		<!-- Clear additional tags button -->
		{#if additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0}
			<button class="text-muted-foreground hover:text-foreground text-xs" onclick={ctx.resetTags}>
				{m.common_clear_tags()}
			</button>
		{/if}
	</div>
{/if}
