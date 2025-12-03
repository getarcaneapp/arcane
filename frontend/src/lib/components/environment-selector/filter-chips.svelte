<script lang="ts">
	import { Badge } from '$lib/components/ui/badge/index.js';
	import XIcon from '@lucide/svelte/icons/x';
	import { m } from '$lib/paraglide/messages';
	import type { EnvironmentFilter } from './types';

	interface Props {
		selectedTags: string[];
		excludedTags: string[];
		statusFilter: 'all' | 'online' | 'offline';
		tagMode: 'any' | 'all';
		activeSavedFilter: EnvironmentFilter | null;
		onRemoveTag?: (tag: string) => void;
		onRemoveExcludedTag?: (tag: string) => void;
		onClearStatus?: () => void;
		onClearSavedFilter?: () => void;
		onClearTags?: () => void;
	}

	let {
		selectedTags,
		excludedTags,
		statusFilter,
		tagMode,
		activeSavedFilter,
		onRemoveTag,
		onRemoveExcludedTag,
		onClearStatus,
		onClearSavedFilter,
		onClearTags
	}: Props = $props();

	// Compute additional filters (those not in the active saved filter)
	const additionalSelectedTags = $derived(
		activeSavedFilter ? selectedTags.filter((t) => !activeSavedFilter.selectedTags.includes(t)) : selectedTags
	);
	const additionalExcludedTags = $derived(
		activeSavedFilter ? excludedTags.filter((t) => !activeSavedFilter.excludedTags.includes(t)) : excludedTags
	);
	const isStatusFilterAdditional = $derived(
		activeSavedFilter ? statusFilter !== activeSavedFilter.statusFilter : statusFilter !== 'all'
	);
	const isTagModeAdditional = $derived(
		activeSavedFilter
			? tagMode !== activeSavedFilter.tagMode && selectedTags.length > 1
			: tagMode !== 'any' && selectedTags.length > 1
	);
	const hasAdditionalFilters = $derived(
		additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0 || isStatusFilterAdditional
	);
	const showChips = $derived(activeSavedFilter || hasAdditionalFilters);
</script>

{#if showChips}
	<div class="flex flex-wrap items-center gap-1.5">
		<!-- Active saved filter badge -->
		{#if activeSavedFilter}
			<Badge variant="secondary" class="gap-1 pr-1">
				{activeSavedFilter.name}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={onClearSavedFilter}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/if}

		<!-- Additional status filter -->
		{#if isStatusFilterAdditional}
			<Badge variant="outline" class="gap-1 pr-1">
				<span class="size-1.5 rounded-full {statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500'}"></span>
				{statusFilter === 'online' ? m.common_online() : m.common_offline()}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={onClearStatus}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/if}

		<!-- Tag mode badge -->
		{#if isTagModeAdditional}
			<Badge variant="outline" class="text-xs"
				>{tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}</Badge
			>
		{/if}

		<!-- Additional selected tags -->
		{#each additionalSelectedTags as tag}
			<Badge variant="outline" class="gap-1 pr-1 text-sky-700 dark:text-sky-300">
				{tag}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => onRemoveTag?.(tag)}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/each}

		<!-- Additional excluded tags -->
		{#each additionalExcludedTags as tag}
			<Badge variant="outline" class="gap-1 pr-1 text-orange-700 dark:text-orange-300">
				{tag}
				<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => onRemoveExcludedTag?.(tag)}>
					<XIcon class="size-3" />
				</button>
			</Badge>
		{/each}

		<!-- Clear additional tags button -->
		{#if additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0}
			<button class="text-muted-foreground hover:text-foreground text-xs" onclick={onClearTags}>
				{m.common_clear_tags()}
			</button>
		{/if}
	</div>
{/if}
