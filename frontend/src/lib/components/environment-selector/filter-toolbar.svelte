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
	import { useEnvSelector } from './context.svelte';
	import SearchSuggestions from './search-suggestions.svelte';
	import type { Suggestion, InputMatch } from './types';

	interface Props {
		inputValue: string;
		suggestions: Suggestion[];
		inputMatch: InputMatch | null;
		selectedSuggestionIndex: number;
		defaultFilterDisabled: boolean;
		onKeydown?: (event: KeyboardEvent) => void;
		onSelectSuggestion?: (index: number) => void;
		onClearFilters?: () => void;
		onResetToDefault?: () => void;
		onShowSavedFilters?: () => void;
	}

	let {
		inputValue = $bindable(),
		suggestions,
		inputMatch,
		selectedSuggestionIndex = $bindable(),
		defaultFilterDisabled,
		onKeydown,
		onSelectSuggestion,
		onClearFilters,
		onResetToDefault,
		onShowSavedFilters
	}: Props = $props();

	const ctx = useEnvSelector();

	// Grouping options
	const canGroupByStatus = $derived(ctx.filters.statusFilter === 'all');
	const canGroupByTags = $derived(
		ctx.allTags.length > 0 && ctx.filters.selectedTags.length === 0 && ctx.filters.excludedTags.length === 0
	);
	const hasGroupingOptions = $derived(canGroupByStatus || canGroupByTags);
	const groupByOptions = $derived([
		{ value: 'none' as const, label: m.common_none() },
		...(canGroupByStatus ? [{ value: 'status' as const, label: m.common_status() }] : []),
		...(canGroupByTags ? [{ value: 'tags' as const, label: m.common_tags() }] : [])
	]);
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
					{#each groupByOptions as opt (opt.value)}
						<button
							class={cn(
								'flex w-full items-center rounded-md px-2 py-1.5 text-sm transition-colors',
								ctx.filters.groupBy === opt.value ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
							)}
							onclick={() => ctx.setGroupBy(opt.value)}
						>
							{opt.label}
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
				<!-- Saved Filters -->
				<Button variant="outline" size="sm" class="h-8 w-full gap-1.5" onclick={onShowSavedFilters}>
					<BookmarkIcon class="size-4" />
					{m.env_selector_saved_filters()}
				</Button>

				<!-- Tag mode (only when multiple tags selected) -->
				{#if ctx.filters.selectedTags.length > 1}
					<div class="space-y-1.5">
						<span class="text-muted-foreground text-xs font-medium">{m.env_selector_tag_mode()}</span>
						<div class="flex gap-1">
							{#each [{ value: 'any', label: m.env_selector_tag_mode_any(), desc: m.env_selector_tag_mode_any_desc() }, { value: 'all', label: m.env_selector_tag_mode_all(), desc: m.env_selector_tag_mode_all_desc() }] as mode (mode.value)}
								<Tooltip.Provider>
									<Tooltip.Root>
										<Tooltip.Trigger class="flex-1">
											<button
												class={cn(
													'w-full rounded-md px-2 py-1.5 text-xs transition-colors',
													ctx.filters.tagMode === mode.value ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
												)}
												onclick={() => ctx.setTagMode(mode.value as 'any' | 'all')}
											>
												{mode.label}
											</button>
										</Tooltip.Trigger>
										<Tooltip.Content>{mode.desc}</Tooltip.Content>
									</Tooltip.Root>
								</Tooltip.Provider>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Clear / Reset -->
				<div class="flex gap-2 border-t pt-3">
					<Button
						variant="ghost"
						size="sm"
						class="h-8 flex-1"
						onclick={onClearFilters}
						disabled={!ctx.hasActiveFilters && !ctx.activeSavedFilter}
					>
						{m.common_clear_filters()}
					</Button>
					{#if defaultFilterDisabled && ctx.hasDefaultFilter}
						<Button variant="ghost" size="sm" class="h-8 flex-1" onclick={onResetToDefault}>
							{m.env_selector_reset_to_default()}
						</Button>
					{/if}
				</div>
			</div>
		</Popover.Content>
	</Popover.Root>
</div>
