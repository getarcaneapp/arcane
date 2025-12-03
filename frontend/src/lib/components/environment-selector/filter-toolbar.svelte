<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import SearchIcon from '@lucide/svelte/icons/search';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import BookmarkIcon from '@lucide/svelte/icons/bookmark';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import SearchSuggestions from './search-suggestions.svelte';
	import type { EnvironmentFilterState, Suggestion, InputMatch, EnvironmentFilter } from './types';

	interface Props {
		inputValue: string;
		filters: EnvironmentFilterState;
		allTags: string[];
		suggestions: Suggestion[];
		inputMatch: InputMatch | null;
		selectedSuggestionIndex: number;
		activeSavedFilter: EnvironmentFilter | null;
		hasDefaultFilter?: boolean;
		defaultFilterDisabled: boolean;
		onKeydown?: (event: KeyboardEvent) => void;
		onSelectSuggestion?: (index: number) => void;
		onFiltersChange?: (filters: Partial<EnvironmentFilterState>) => void;
		onClearFilters?: () => void;
		onResetToDefault?: () => void;
		onShowSavedFilters?: () => void;
	}

	let {
		inputValue = $bindable(),
		filters,
		allTags,
		suggestions,
		inputMatch,
		selectedSuggestionIndex = $bindable(),
		activeSavedFilter,
		hasDefaultFilter = false,
		defaultFilterDisabled,
		onKeydown,
		onSelectSuggestion,
		onFiltersChange,
		onClearFilters,
		onResetToDefault,
		onShowSavedFilters
	}: Props = $props();

	// Compute available grouping options
	const canGroupByStatus = $derived(filters.statusFilter === 'all');
	const canGroupByTags = $derived(allTags.length > 0 && filters.selectedTags.length === 0 && filters.excludedTags.length === 0);
	const hasGroupingOptions = $derived(canGroupByStatus || canGroupByTags);

	const groupByOptions = $derived([
		{ value: 'none' as const, label: m.common_none() },
		...(canGroupByStatus ? [{ value: 'status' as const, label: m.common_status() }] : []),
		...(canGroupByTags ? [{ value: 'tags' as const, label: m.common_tags() }] : [])
	]);

	const hasActiveFilters = $derived(
		filters.statusFilter !== 'all' ||
			filters.selectedTags.length > 0 ||
			filters.excludedTags.length > 0 ||
			filters.tagMode !== 'any' ||
			filters.groupBy !== 'none'
	);
</script>

<div class="flex items-center gap-2">
	<!-- Search input -->
	<div class="relative flex-1">
		<SearchIcon class="text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2" />
		<Input type="text" placeholder={m.common_search()} class="h-9 pl-9 text-sm" bind:value={inputValue} onkeydown={onKeydown} />
		<SearchSuggestions {suggestions} {inputMatch} bind:selectedIndex={selectedSuggestionIndex} onSelect={onSelectSuggestion} />
	</div>

	<!-- Group button -->
	{#if hasGroupingOptions}
		<Popover.Root>
			<Popover.Trigger>
				<Button variant="outline" size="sm" class="h-9 gap-1.5">
					<LayersIcon class="size-4" />
					<span class="hidden sm:inline">{m.env_selector_group()}</span>
				</Button>
			</Popover.Trigger>
			<Popover.Content class="w-48 p-2" align="start">
				<div class="space-y-1">
					{#each groupByOptions as option}
						<button
							class={cn(
								'flex w-full items-center rounded-md px-2 py-1.5 text-sm transition-colors',
								filters.groupBy === option.value ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
							)}
							onclick={() => onFiltersChange?.({ groupBy: option.value })}
						>
							{option.label}
						</button>
					{/each}
				</div>
			</Popover.Content>
		</Popover.Root>
	{/if}

	<!-- Filters button -->
	<Popover.Root>
		<Popover.Trigger>
			<Button variant="outline" size="sm" class="h-9 gap-1.5">
				<FilterIcon class="size-4" />
				<span class="hidden sm:inline">{m.common_filters()}</span>
			</Button>
		</Popover.Trigger>
		<Popover.Content class="w-64 p-3" align="end">
			<div class="space-y-3">
				<!-- Saved Filters button -->
				<Button variant="outline" size="sm" class="h-8 w-full gap-1.5" onclick={onShowSavedFilters}>
					<BookmarkIcon class="size-4" />
					{m.env_selector_saved_filters()}
				</Button>

				<!-- Tag matching mode -->
				{#if filters.selectedTags.length > 1}
					<div class="space-y-1.5">
						<span class="text-muted-foreground text-xs font-medium">{m.env_selector_tag_mode()}</span>
						<div class="flex gap-1">
							<Tooltip.Provider>
								<Tooltip.Root>
									<Tooltip.Trigger class="flex-1">
										<button
											class={cn(
												'w-full rounded-md px-2 py-1.5 text-xs transition-colors',
												filters.tagMode === 'any' ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => onFiltersChange?.({ tagMode: 'any' })}
										>
											{m.env_selector_tag_mode_any()}
										</button>
									</Tooltip.Trigger>
									<Tooltip.Content>{m.env_selector_tag_mode_any_desc()}</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
							<Tooltip.Provider>
								<Tooltip.Root>
									<Tooltip.Trigger class="flex-1">
										<button
											class={cn(
												'w-full rounded-md px-2 py-1.5 text-xs transition-colors',
												filters.tagMode === 'all' ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => onFiltersChange?.({ tagMode: 'all' })}
										>
											{m.env_selector_tag_mode_all()}
										</button>
									</Tooltip.Trigger>
									<Tooltip.Content>{m.env_selector_tag_mode_all_desc()}</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
						</div>
					</div>
				{/if}

				<!-- Clear / Reset buttons -->
				<div class="flex gap-2 border-t pt-3">
					<Button
						variant="ghost"
						size="sm"
						class="h-8 flex-1"
						onclick={onClearFilters}
						disabled={!hasActiveFilters && !activeSavedFilter}
					>
						{m.common_clear_filters()}
					</Button>
					{#if defaultFilterDisabled && hasDefaultFilter}
						<Button variant="ghost" size="sm" class="h-8 flex-1" onclick={onResetToDefault}>
							{m.env_selector_reset_to_default()}
						</Button>
					{/if}
				</div>
			</div>
		</Popover.Content>
	</Popover.Root>
</div>
