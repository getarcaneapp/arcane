<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import SearchIcon from '@lucide/svelte/icons/search';
	import ServerIcon from '@lucide/svelte/icons/server';
	import RouterIcon from '@lucide/svelte/icons/router';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import TagIcon from '@lucide/svelte/icons/tag';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import StarIcon from '@lucide/svelte/icons/star';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import SaveIcon from '@lucide/svelte/icons/save';
	import BookmarkIcon from '@lucide/svelte/icons/bookmark';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import type { Environment } from '$lib/types/environment.type';
	import type { EnvironmentFilter } from '$lib/types/environment.type';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { debounced } from '$lib/utils/utils';
	import { goto } from '$app/navigation';
	import settingsStore from '$lib/stores/config-store';
	import type { PaginationResponse } from '$lib/types/pagination.type';

	interface Props {
		open?: boolean;
		isAdmin?: boolean;
		onOpenChange?: (open: boolean) => void;
		trigger?: import('svelte').Snippet;
	}

	let { open = $bindable(false), isAdmin = false, onOpenChange, trigger }: Props = $props();

	interface EnvironmentFilterState {
		selectedTags: string[];
		excludedTags: string[];
		tagMode: 'any' | 'all';
		statusFilter: 'all' | 'online' | 'offline';
		groupBy: 'none' | 'status' | 'tags';
	}

	const defaultFilterState: EnvironmentFilterState = {
		selectedTags: [],
		excludedTags: [],
		tagMode: 'any',
		statusFilter: 'all',
		groupBy: 'none'
	};

	// Environment data
	let environments = $state<Environment[]>([]);
	let pagination = $state<PaginationResponse | null>(null);
	let allTags = $state<string[]>([]);
	let isLoading = $state(true);

	// Search & filter state (local, not persisted)
	let inputValue = $state('');
	let filters = $state<EnvironmentFilterState>({
		selectedTags: [],
		excludedTags: [],
		tagMode: 'any',
		statusFilter: 'all',
		groupBy: 'none'
	});

	// Saved filters
	let savedFilters = $state<EnvironmentFilter[]>([]);
	let activeFilterId = $state<string | null>(null);
	let defaultFilterDisabled = $state(false); // Prevents auto-applying default filter

	// UI state
	let filterPopoverOpen = $state(false);
	let searchInputRef = $state<HTMLInputElement | null>(null);
	let selectedSuggestionIndex = $state(0);
	let showSavedFiltersView = $state(false);
	let saveFilterName = $state('');
	let editingFilterId = $state<string | null>(null);
	let editingFilterName = $state('');

	const searchQuery = $derived(
		inputValue
			.replace(/-?tag:\S*/gi, '')
			.replace(/is:\S*/gi, '')
			.trim()
	);
	const inputMatch = $derived.by(() => {
		const isMatch = inputValue.match(/is:(\S*)$/i);
		if (isMatch) return { type: 'status' as const, partial: isMatch[1].toLowerCase() };

		const excludeMatch = inputValue.match(/-tag:(\S*)$/i);
		if (excludeMatch) return { type: 'exclude' as const, partial: excludeMatch[1].toLowerCase() };

		const includeMatch = inputValue.match(/tag:(\S*)$/i);
		if (includeMatch) return { type: 'include' as const, partial: includeMatch[1].toLowerCase() };

		return null;
	});
	const suggestions = $derived.by(() => {
		if (!inputMatch) return [];

		if (inputMatch.type === 'status') {
			const statusOptions = [
				{ value: 'online', label: m.common_online() },
				{ value: 'offline', label: m.common_offline() }
			];
			return statusOptions.filter(
				(s) =>
					(s.value.includes(inputMatch.partial) || s.label.toLowerCase().includes(inputMatch.partial)) &&
					filters.statusFilter !== s.value
			);
		}

		// Tag suggestions - exclude already used tags
		const usedTags = new Set([...filters.selectedTags, ...filters.excludedTags]);
		return allTags
			.filter((tag) => !usedTags.has(tag) && tag.toLowerCase().includes(inputMatch.partial))
			.map((tag) => ({ value: tag, label: tag }));
	});

	// Reset suggestion index when suggestions change
	$effect(() => {
		selectedSuggestionIndex = suggestions.length > 0 ? 0 : -1;
	});

	// Reset groupBy when it becomes invalid due to filter changes
	$effect(() => {
		const canGroupByStatus = filters.statusFilter === 'all';
		const canGroupByTags = allTags.length > 0 && filters.selectedTags.length === 0 && filters.excludedTags.length === 0;

		if (filters.groupBy === 'status' && !canGroupByStatus) {
			filters = { ...filters, groupBy: 'none' };
		} else if (filters.groupBy === 'tags' && !canGroupByTags) {
			filters = { ...filters, groupBy: 'none' };
		}
	});

	// Grouping (client-side, on already-filtered data from backend)
	const groupedEnvironments = $derived.by(() => {
		if (filters.groupBy === 'none') return null;

		const groups = new Map<string, Environment[]>();

		for (const env of environments) {
			const keys = filters.groupBy === 'status' ? [env.status] : env.tags?.length ? env.tags : [m.common_none()];

			for (const key of keys) {
				const items = groups.get(key) ?? [];
				if (!items.some((e) => e.id === env.id)) items.push(env);
				groups.set(key, items);
			}
		}

		return [...groups.entries()]
			.map(([name, items]) => ({ name, items }))
			.sort((a, b) => (a.name === 'online' ? -1 : b.name === 'online' ? 1 : a.name.localeCompare(b.name)));
	});

	const hasActiveFilters = $derived(
		filters.selectedTags.length > 0 ||
			filters.excludedTags.length > 0 ||
			filters.statusFilter !== 'all' ||
			filters.groupBy !== 'none'
	);
	const hasMorePages = $derived(pagination ? pagination.currentPage < pagination.totalPages : false);

	// Get the active saved filter object
	const activeSavedFilter = $derived(activeFilterId ? savedFilters.find((f) => f.id === activeFilterId) : null);

	// Compute additional filters (those not in the active saved filter)
	const additionalSelectedTags = $derived(
		activeSavedFilter ? filters.selectedTags.filter((t) => !activeSavedFilter.selectedTags.includes(t)) : filters.selectedTags
	);
	const additionalExcludedTags = $derived(
		activeSavedFilter ? filters.excludedTags.filter((t) => !activeSavedFilter.excludedTags.includes(t)) : filters.excludedTags
	);
	const isStatusFilterAdditional = $derived(
		activeSavedFilter ? filters.statusFilter !== activeSavedFilter.statusFilter : filters.statusFilter !== 'all'
	);
	const isTagModeAdditional = $derived(
		activeSavedFilter
			? filters.tagMode !== activeSavedFilter.tagMode && filters.selectedTags.length > 1
			: filters.tagMode !== 'any' && filters.selectedTags.length > 1
	);
	const hasAdditionalFilters = $derived(
		additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0 || isStatusFilterAdditional
	);

	// Check if a saved filter differs from current filters (for showing update button)
	function isFilterDifferent(savedFilter: EnvironmentFilter): boolean {
		const tagsMatch =
			filters.selectedTags.length === savedFilter.selectedTags.length &&
			filters.selectedTags.every((t) => savedFilter.selectedTags.includes(t));
		const excludedMatch =
			filters.excludedTags.length === savedFilter.excludedTags.length &&
			filters.excludedTags.every((t) => savedFilter.excludedTags.includes(t));

		return (
			!tagsMatch ||
			!excludedMatch ||
			filters.tagMode !== savedFilter.tagMode ||
			filters.statusFilter !== savedFilter.statusFilter ||
			filters.groupBy !== savedFilter.groupBy
		);
	}

	// Debounced load function
	const debouncedLoad = debounced(() => {
		if (open) loadEnvironments();
	}, 300);

	// Track previous filter values to detect actual changes
	let prevFilterKey = $state('');

	// Compute a filter key for comparison
	const filterKey = $derived(
		JSON.stringify([searchQuery, filters.statusFilter, filters.selectedTags, filters.excludedTags, filters.tagMode])
	);

	$effect(() => {
		if (open) {
			loadEnvironments();
			loadAllTags();
			loadSavedFilters();
		} else {
			isLoading = true;
			showSavedFiltersView = false;
		}
	});

	$effect(() => {
		const currentKey = filterKey;
		if (open && prevFilterKey && prevFilterKey !== currentKey) {
			debouncedLoad();
		}
		prevFilterKey = currentKey;
	});

	async function loadEnvironments(page = 1) {
		try {
			const result = await environmentManagementService.getEnvironments({
				search: searchQuery || undefined,
				pagination: { page, limit: 50 },
				sort: { column: 'name', direction: 'asc' },
				tags: filters.selectedTags.length > 0 ? filters.selectedTags : undefined,
				excludeTags: filters.excludedTags.length > 0 ? filters.excludedTags : undefined,
				tagMode: filters.tagMode !== 'any' ? filters.tagMode : undefined,
				status: filters.statusFilter !== 'all' ? filters.statusFilter : undefined
			});

			environments = page === 1 ? result.data : [...environments, ...result.data];
			pagination = result.pagination;
		} catch (error) {
			console.error('Failed to load environments:', error);
			toast.error(m.common_refresh_failed({ resource: m.environments_title() }));
		} finally {
			isLoading = false;
		}
	}

	async function loadAllTags() {
		try {
			allTags = await environmentManagementService.getAllTags();
		} catch (error) {
			console.error('Failed to load tags:', error);
		}
	}

	async function loadSavedFilters() {
		try {
			savedFilters = await environmentManagementService.getSavedFilters();
			// Apply default filter if exists, no filter is active, and default is not disabled
			if (!activeFilterId && !defaultFilterDisabled) {
				const defaultFilter = savedFilters.find((f) => f.isDefault);
				if (defaultFilter) {
					applyFilter(defaultFilter);
				}
			}
		} catch (error) {
			console.error('Failed to load saved filters:', error);
		}
	}

	function loadMore() {
		if (hasMorePages && !isLoading) {
			loadEnvironments((pagination?.currentPage ?? 0) + 1);
		}
	}

	function handleKeydown(event: KeyboardEvent) {
		if (suggestions.length > 0) {
			if (event.key === 'ArrowDown') {
				event.preventDefault();
				selectedSuggestionIndex = Math.min(selectedSuggestionIndex + 1, suggestions.length - 1);
			} else if (event.key === 'ArrowUp') {
				event.preventDefault();
				selectedSuggestionIndex = Math.max(selectedSuggestionIndex - 1, 0);
			} else if (event.key === 'Tab' || event.key === 'Enter') {
				event.preventDefault();
				selectSuggestion(selectedSuggestionIndex);
			} else if (event.key === 'Escape') {
				inputValue = searchQuery;
			}
		}
	}

	function selectSuggestion(index: number) {
		const suggestion = suggestions[index];
		if (!suggestion || !inputMatch) return;

		if (inputMatch.type === 'status') {
			filters = { ...filters, statusFilter: suggestion.value as 'online' | 'offline' };
			// Don't clear activeFilterId - allow adding on top of saved filter
		} else if (inputMatch.type === 'exclude') {
			addExcludedTag(suggestion.value);
		} else {
			addTag(suggestion.value);
		}

		inputValue = searchQuery;
		searchInputRef?.focus();
	}

	function addTag(tag: string) {
		if (!filters.selectedTags.includes(tag) && !filters.excludedTags.includes(tag)) {
			filters = { ...filters, selectedTags: [...filters.selectedTags, tag] };
			// Don't clear activeFilterId - allow adding on top of saved filter
		}
	}

	function removeTag(tag: string) {
		filters = { ...filters, selectedTags: filters.selectedTags.filter((t) => t !== tag) };
		// Don't clear activeFilterId - allow removing from saved filter
	}

	function addExcludedTag(tag: string) {
		if (!filters.excludedTags.includes(tag) && !filters.selectedTags.includes(tag)) {
			filters = { ...filters, excludedTags: [...filters.excludedTags, tag] };
			// Don't clear activeFilterId - allow adding on top of saved filter
		}
	}

	function removeExcludedTag(tag: string) {
		filters = { ...filters, excludedTags: filters.excludedTags.filter((t) => t !== tag) };
		// Don't clear activeFilterId - allow removing from saved filter
	}

	function clearTags() {
		if (activeSavedFilter) {
			// Reset to saved filter's tags
			filters = {
				...filters,
				selectedTags: [...activeSavedFilter.selectedTags],
				excludedTags: [...activeSavedFilter.excludedTags]
			};
		} else {
			filters = { ...filters, selectedTags: [], excludedTags: [] };
		}
	}

	function clearFilters() {
		filters = { selectedTags: [], excludedTags: [], tagMode: 'any', statusFilter: 'all', groupBy: 'none' };
		activeFilterId = null;
		defaultFilterDisabled = true; // Prevent default from being reapplied
	}

	function clearSavedFilter() {
		// Clear the active saved filter but keep any additional filters
		activeFilterId = null;
		defaultFilterDisabled = true; // Prevent default from being reapplied
	}

	function resetToDefault() {
		// Re-enable default filter and apply it if exists
		defaultFilterDisabled = false;
		const defaultFilter = savedFilters.find((f) => f.isDefault);
		if (defaultFilter) {
			applyFilter(defaultFilter);
		} else {
			// No default, just clear everything
			filters = { selectedTags: [], excludedTags: [], tagMode: 'any', statusFilter: 'all', groupBy: 'none' };
			activeFilterId = null;
		}
		inputValue = '';
	}

	async function handleSelectEnvironment(env: Environment) {
		if (!env.enabled) {
			toast.error(m.environments_cannot_switch_disabled());
			return;
		}

		try {
			await environmentStore.setEnvironment(env);
			toast.success(m.environments_switched_to({ name: env.name }));
			open = false;
			onOpenChange?.(false);
		} catch (error) {
			console.error('Failed to set environment:', error);
			toast.error(m.env_selector_switch_failed());
		}
	}

	function handleClearFilters() {
		inputValue = '';
		clearFilters();
	}

	function getConnectionString(env: Environment): string {
		return env.id === '0' ? $settingsStore.dockerHost || 'unix:///var/run/docker.sock' : env.apiUrl;
	}

	function getStatusColor(status: string): string {
		return status === 'online' ? 'bg-emerald-500' : status === 'offline' ? 'bg-red-500' : 'bg-gray-400';
	}

	function applyFilter(filter: EnvironmentFilter) {
		filters = {
			selectedTags: filter.selectedTags ?? [],
			excludedTags: filter.excludedTags ?? [],
			tagMode: filter.tagMode,
			statusFilter: filter.statusFilter,
			groupBy: filter.groupBy
		};
		activeFilterId = filter.id;
		showSavedFiltersView = false;
	}

	async function handleSaveFilter() {
		if (!saveFilterName.trim()) return;

		try {
			const created = await environmentManagementService.createSavedFilter({
				name: saveFilterName.trim(),
				selectedTags: filters.selectedTags,
				excludedTags: filters.excludedTags,
				tagMode: filters.tagMode,
				statusFilter: filters.statusFilter,
				groupBy: filters.groupBy
			});
			savedFilters = [...savedFilters, created].sort((a, b) => a.name.localeCompare(b.name));
			activeFilterId = created.id;
			toast.success(m.common_create_success({ resource: m.common_filter() }));
			saveFilterName = '';
		} catch (error) {
			console.error('Failed to save filter:', error);
			toast.error(m.common_create_failed({ resource: m.common_filter() }));
		}
	}

	async function handleUpdateFilter(filterId: string) {
		try {
			const updated = await environmentManagementService.updateSavedFilter(filterId, {
				selectedTags: filters.selectedTags,
				excludedTags: filters.excludedTags,
				tagMode: filters.tagMode,
				statusFilter: filters.statusFilter,
				groupBy: filters.groupBy
			});
			savedFilters = savedFilters.map((f) => (f.id === filterId ? updated : f));
			toast.success(m.common_update_success({ resource: m.common_filter() }));
		} catch (error) {
			console.error('Failed to update filter:', error);
			toast.error(m.common_update_failed({ resource: m.common_filter() }));
		}
	}

	async function handleDeleteFilter(filterId: string) {
		try {
			await environmentManagementService.deleteSavedFilter(filterId);
			savedFilters = savedFilters.filter((f) => f.id !== filterId);
			if (activeFilterId === filterId) activeFilterId = null;
			toast.success(m.common_delete_success({ resource: m.common_filter() }));
		} catch (error) {
			console.error('Failed to delete filter:', error);
			toast.error(m.common_delete_failed({ resource: m.common_filter() }));
		}
	}

	async function handleSetFilterDefault(filterId: string) {
		try {
			await environmentManagementService.setSavedFilterDefault(filterId);
			savedFilters = savedFilters.map((f) => ({ ...f, isDefault: f.id === filterId }));
			defaultFilterDisabled = false; // Re-enable default since user explicitly set one
			toast.success(m.common_update_success({ resource: m.common_filter() }));
		} catch (error) {
			console.error('Failed to set default filter:', error);
			toast.error(m.common_update_failed({ resource: m.common_filter() }));
		}
	}

	async function handleClearFilterDefault(filterId: string) {
		try {
			await environmentManagementService.clearSavedFilterDefault();
			savedFilters = savedFilters.map((f) => ({ ...f, isDefault: false }));
			toast.success(m.common_update_success({ resource: m.common_filter() }));
		} catch (error) {
			console.error('Failed to clear default filter:', error);
			toast.error(m.common_update_failed({ resource: m.common_filter() }));
		}
	}

	function startEditingFilter(filter: EnvironmentFilter) {
		editingFilterId = filter.id;
		editingFilterName = filter.name;
	}

	function cancelEditingFilter() {
		editingFilterId = null;
		editingFilterName = '';
	}

	async function handleRenameFilter() {
		if (!editingFilterId || !editingFilterName.trim()) return;

		try {
			const updated = await environmentManagementService.updateSavedFilter(editingFilterId, {
				name: editingFilterName.trim()
			});
			savedFilters = savedFilters.map((f) => (f.id === editingFilterId ? updated : f));
			toast.success(m.common_update_success({ resource: m.common_filter() }));
			cancelEditingFilter();
		} catch (error) {
			console.error('Failed to rename filter:', error);
			toast.error(m.common_update_failed({ resource: m.common_filter() }));
		}
	}
</script>

{#snippet environmentItem(env: Environment)}
	{@const isSelected = environmentStore.selected?.id === env.id}
	{@const isDisabled = !env.enabled}
	<button
		class={cn(
			'group relative flex w-full items-center gap-3 rounded-lg p-2.5 text-left transition-all',
			isSelected ? 'bg-primary/10 ring-primary/40 ring-1' : isDisabled ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted/60'
		)}
		onclick={() => handleSelectEnvironment(env)}
		disabled={isDisabled}
	>
		<div class="relative">
			<div
				class={cn(
					'flex size-9 items-center justify-center rounded-md',
					isSelected ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
				)}
			>
				{#if env.id === '0'}<ServerIcon class="size-4" />{:else}<RouterIcon class="size-4" />{/if}
			</div>
			<span class={cn('ring-background absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2', getStatusColor(env.status))}
			></span>
		</div>

		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-2">
				<span class={cn('truncate text-sm font-medium', isSelected && 'text-primary')}>{env.name}</span>
				{#if isSelected}<CheckIcon class="text-primary size-4 shrink-0" />{/if}
			</div>
			<div class="text-muted-foreground truncate text-xs">{getConnectionString(env)}</div>
		</div>

		{#if env.tags?.length}
			<div class="hidden shrink-0 sm:flex sm:gap-1">
				{#each env.tags.slice(0, 2) as tag}
					<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px]">{tag}</span>
				{/each}
				{#if env.tags.length > 2}
					<span class="text-muted-foreground text-[10px]">+{env.tags.length - 2}</span>
				{/if}
			</div>
		{/if}
	</button>
{/snippet}

{#snippet envGroup(group: { name: string; items: Environment[] })}
	<Collapsible.Root class="w-full" open={false}>
		<Collapsible.Trigger
			class="hover:bg-muted/40 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors"
		>
			<ChevronDownIcon class="text-muted-foreground size-4 transition-transform in-data-[state=closed]:-rotate-90" />
			<span class="text-sm font-medium">{group.name}</span>
			<span class="text-muted-foreground text-xs">({group.items.length})</span>
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="mt-1 space-y-0.5 pl-2">
				{#each group.items as env (env.id)}
					{@render environmentItem(env)}
				{/each}
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
{/snippet}

<ResponsiveDialog bind:open {onOpenChange} {trigger} title={m.env_selector_title()} contentClass="sm:max-w-2xl">
	{#snippet children()}
		{#if showSavedFiltersView}
			<!-- Saved Filters View -->
			<div class="flex flex-col gap-3">
				<div class="flex items-center gap-2">
					<Button variant="ghost" size="sm" class="h-8 gap-1 px-2" onclick={() => (showSavedFiltersView = false)}>
						<ChevronLeftIcon class="size-4" />
						{m.common_back()}
					</Button>
					<span class="text-sm font-medium">{m.env_selector_saved_filters()}</span>
				</div>

				<!-- Save Current Filter -->
				{#if hasActiveFilters}
					<div class="flex gap-2">
						<Input
							type="text"
							placeholder={m.env_selector_filter_name_placeholder()}
							class="h-9 text-sm"
							bind:value={saveFilterName}
							onkeydown={(e) => e.key === 'Enter' && handleSaveFilter()}
						/>
						<Button size="sm" class="h-9 gap-1.5" onclick={handleSaveFilter} disabled={!saveFilterName.trim()}>
							<PlusIcon class="size-4" />
							{m.common_save()}
						</Button>
					</div>
				{/if}

				<!-- Saved Filters List -->
				<ScrollArea class="max-h-[50vh] min-h-[180px]">
					{#if savedFilters.length === 0}
						<div class="text-muted-foreground flex h-40 flex-col items-center justify-center text-center">
							<FilterIcon class="mb-2 size-8 opacity-30" />
							<p class="text-sm">{m.env_selector_no_saved_filters()}</p>
							<p class="mt-1 text-xs">{m.env_selector_save_filter_hint()}</p>
						</div>
					{:else}
						<div class="space-y-1 p-1">
							{#each savedFilters as filter (filter.id)}
								{@const isActive = activeFilterId === filter.id}
								{@const isEditing = editingFilterId === filter.id}
								<div
									class={cn(
										'group flex items-center gap-3 rounded-lg p-2.5 transition-all',
										isActive ? 'bg-primary/10 ring-primary/40 ring-1' : 'hover:bg-muted/60'
									)}
								>
									{#if isEditing}
										<!-- Editing mode -->
										<div class="flex min-w-0 flex-1 items-center gap-2">
											<Input
												type="text"
												class="h-8 text-sm"
												bind:value={editingFilterName}
												onkeydown={(e) => {
													if (e.key === 'Enter') handleRenameFilter();
													if (e.key === 'Escape') cancelEditingFilter();
												}}
											/>
											<Button size="sm" class="h-8 px-2" onclick={handleRenameFilter} disabled={!editingFilterName.trim()}>
												<CheckIcon class="size-4" />
											</Button>
											<Button variant="ghost" size="sm" class="h-8 px-2" onclick={cancelEditingFilter}>
												<XIcon class="size-4" />
											</Button>
										</div>
									{:else}
										<!-- Normal mode -->
										<button class="flex min-w-0 flex-1 items-center gap-3 text-left" onclick={() => applyFilter(filter)}>
											<div
												class={cn(
													'flex size-9 items-center justify-center rounded-md',
													isActive ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
												)}
											>
												<FilterIcon class="size-4" />
											</div>
											<div class="min-w-0 flex-1">
												<div class="flex items-center gap-2">
													<span class={cn('truncate text-sm font-medium', isActive && 'text-primary')}>{filter.name}</span>
													{#if filter.isDefault}
														<StarIcon class="size-3 shrink-0 fill-yellow-500 text-yellow-500" />
													{/if}
													{#if isActive}
														<CheckIcon class="text-primary size-4 shrink-0" />
													{/if}
												</div>
												<div class="mt-1 flex flex-wrap items-center gap-1">
													{#if filter.statusFilter !== 'all'}
														<span
															class={cn(
																'inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium',
																filter.statusFilter === 'online'
																	? 'bg-emerald-500/15 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'
																	: 'bg-red-500/15 text-red-700 dark:bg-red-500/20 dark:text-red-300'
															)}
														>
															<span
																class={cn(
																	'size-1.5 rounded-full',
																	filter.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500'
																)}
															></span>
															{filter.statusFilter === 'online' ? m.common_online() : m.common_offline()}
														</span>
													{/if}
													{#if filter.selectedTags.length > 0}
														{#each filter.selectedTags.slice(0, 2) as tag}
															<span
																class="rounded bg-sky-500/15 px-1.5 py-0.5 text-[10px] font-medium text-sky-700 dark:bg-sky-500/20 dark:text-sky-300"
																>{tag}</span
															>
														{/each}
														{#if filter.selectedTags.length > 2}
															<Tooltip.Provider>
																<Tooltip.Root>
																	<Tooltip.Trigger>
																		<span
																			class="rounded bg-sky-500/15 px-1.5 py-0.5 text-[10px] font-medium text-sky-700 dark:bg-sky-500/20 dark:text-sky-300"
																			>+{filter.selectedTags.length - 2}</span
																		>
																	</Tooltip.Trigger>
																	<Tooltip.Content>
																		<p class="text-xs">{filter.selectedTags.slice(2).join(', ')}</p>
																	</Tooltip.Content>
																</Tooltip.Root>
															</Tooltip.Provider>
														{/if}
													{/if}
													{#if filter.excludedTags.length > 0}
														{#each filter.excludedTags.slice(0, 2) as tag}
															<span
																class="rounded bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-medium text-orange-700 dark:bg-orange-500/20 dark:text-orange-300"
																>{tag}</span
															>
														{/each}
														{#if filter.excludedTags.length > 2}
															<Tooltip.Provider>
																<Tooltip.Root>
																	<Tooltip.Trigger>
																		<span
																			class="rounded bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-medium text-orange-700 dark:bg-orange-500/20 dark:text-orange-300"
																			>+{filter.excludedTags.length - 2}</span
																		>
																	</Tooltip.Trigger>
																	<Tooltip.Content>
																		<p class="text-xs">{filter.excludedTags.slice(2).join(', ')}</p>
																	</Tooltip.Content>
																</Tooltip.Root>
															</Tooltip.Provider>
														{/if}
													{/if}
													{#if filter.groupBy !== 'none'}
														<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px] font-medium">
															{filter.groupBy === 'status' ? m.common_status() : m.common_tags()}
														</span>
													{/if}
													{#if filter.statusFilter === 'all' && filter.selectedTags.length === 0 && filter.excludedTags.length === 0 && filter.groupBy === 'none'}
														<span class="text-muted-foreground text-xs">{m.common_all()}</span>
													{/if}
												</div>
											</div>
										</button>

										<Tooltip.Provider>
											<div class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
												{#if filter.isDefault}
													<Tooltip.Root>
														<Tooltip.Trigger>
															<Button
																variant="ghost"
																size="sm"
																class="size-7 p-0 text-yellow-500 hover:text-yellow-600"
																onclick={() => handleClearFilterDefault(filter.id)}
															>
																<StarIcon class="size-3.5 fill-current" />
															</Button>
														</Tooltip.Trigger>
														<Tooltip.Content>{m.env_selector_clear_default()}</Tooltip.Content>
													</Tooltip.Root>
												{:else}
													<Tooltip.Root>
														<Tooltip.Trigger>
															<Button
																variant="ghost"
																size="sm"
																class="size-7 p-0"
																onclick={() => handleSetFilterDefault(filter.id)}
															>
																<StarIcon class="size-3.5" />
															</Button>
														</Tooltip.Trigger>
														<Tooltip.Content>{m.env_selector_set_as_default()}</Tooltip.Content>
													</Tooltip.Root>
												{/if}
												<Tooltip.Root>
													<Tooltip.Trigger>
														<Button variant="ghost" size="sm" class="size-7 p-0" onclick={() => startEditingFilter(filter)}>
															<PencilIcon class="size-3.5" />
														</Button>
													</Tooltip.Trigger>
													<Tooltip.Content>{m.common_rename()}</Tooltip.Content>
												</Tooltip.Root>
												{#if isFilterDifferent(filter)}
													<Tooltip.Root>
														<Tooltip.Trigger>
															<Button variant="ghost" size="sm" class="size-7 p-0" onclick={() => handleUpdateFilter(filter.id)}>
																<SaveIcon class="size-3.5" />
															</Button>
														</Tooltip.Trigger>
														<Tooltip.Content>{m.env_selector_update_filter()}</Tooltip.Content>
													</Tooltip.Root>
												{/if}
												<Tooltip.Root>
													<Tooltip.Trigger>
														<Button
															variant="ghost"
															size="sm"
															class="size-7 p-0 text-red-500 hover:text-red-600"
															onclick={() => handleDeleteFilter(filter.id)}
														>
															<Trash2Icon class="size-3.5" />
														</Button>
													</Tooltip.Trigger>
													<Tooltip.Content>{m.common_delete()}</Tooltip.Content>
												</Tooltip.Root>
											</div>
										</Tooltip.Provider>
									{/if}
								</div>
							{/each}
						</div>
					{/if}
				</ScrollArea>
			</div>
		{:else}
			{@const canGroupByStatus = filters.statusFilter === 'all'}
			{@const canGroupByTags = allTags.length > 0 && filters.selectedTags.length === 0 && filters.excludedTags.length === 0}
			{@const hasGroupingOptions = canGroupByStatus || canGroupByTags}

			<!-- Main Environment List View -->
			<div class="flex flex-col gap-3">
				<!-- Search + Filter -->
				<div class="flex gap-2 pt-2">
					<div class="relative flex-1">
						<SearchIcon class="text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2" />
						<Input
							bind:ref={searchInputRef}
							type="text"
							placeholder={m.common_search()}
							class="h-9 pr-8 pl-9 text-sm"
							bind:value={inputValue}
							onkeydown={handleKeydown}
						/>

						<!-- Suggestions Dropdown -->
						{#if suggestions.length > 0}
							<div class="bg-popover border-border absolute top-full right-0 left-0 z-50 mt-1 rounded-md border shadow-md">
								<div class="text-muted-foreground px-3 py-1.5 text-xs">{m.env_selector_suggestions()}</div>
								<div class="max-h-[180px] overflow-y-auto p-1 pt-0">
									{#each suggestions as suggestion, index}
										<button
											class={cn(
												'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors',
												index === selectedSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
											)}
											onclick={() => selectSuggestion(index)}
											onmouseenter={() => (selectedSuggestionIndex = index)}
										>
											{#if inputMatch?.type === 'status'}
												<span class={cn('size-2 rounded-full', suggestion.value === 'online' ? 'bg-emerald-500' : 'bg-red-500')}
												></span>
											{:else}
												<TagIcon class="size-3.5" />
											{/if}
											<span>{suggestion.label}</span>
										</button>
									{/each}
								</div>
							</div>
						{/if}
					</div>

					<!-- Group Popover -->
					{#if hasGroupingOptions}
						<Popover.Root>
							<Popover.Trigger>
								<Button variant="outline" size="sm" class="h-9 gap-1.5">
									<LayersIcon class="size-4" />
									<span class="hidden sm:inline">{m.env_selector_group()}</span>
								</Button>
							</Popover.Trigger>
							<Popover.Content class="w-48 p-2" align="end">
								<div class="space-y-1">
									<button
										class={cn(
											'flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors',
											filters.groupBy === 'none' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
										)}
										onclick={() => (filters = { ...filters, groupBy: 'none' })}
									>
										{#if filters.groupBy === 'none'}
											<CheckIcon class="size-4" />
										{:else}
											<span class="size-4"></span>
										{/if}
										{m.common_none()}
									</button>
									{#if canGroupByStatus}
										<button
											class={cn(
												'flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors',
												filters.groupBy === 'status' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
											)}
											onclick={() => (filters = { ...filters, groupBy: 'status' })}
										>
											{#if filters.groupBy === 'status'}
												<CheckIcon class="size-4" />
											{:else}
												<span class="size-4"></span>
											{/if}
											{m.common_status()}
										</button>
									{/if}
									{#if canGroupByTags}
										<button
											class={cn(
												'flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors',
												filters.groupBy === 'tags' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
											)}
											onclick={() => (filters = { ...filters, groupBy: 'tags' })}
										>
											{#if filters.groupBy === 'tags'}
												<CheckIcon class="size-4" />
											{:else}
												<span class="size-4"></span>
											{/if}
											{m.common_tags()}
										</button>
									{/if}
								</div>
							</Popover.Content>
						</Popover.Root>
					{/if}

					<!-- Filters Popover -->
					<Popover.Root bind:open={filterPopoverOpen}>
						<Popover.Trigger>
							<Button variant="outline" size="sm" class="h-9 gap-1.5">
								<FilterIcon class="size-4" />
								<span class="hidden sm:inline">{m.common_filters()}</span>
							</Button>
						</Popover.Trigger>
						<Popover.Content class="w-56 p-2" align="end">
							<div class="space-y-1">
								<!-- Saved Filters -->
								<Button
									variant="ghost"
									size="sm"
									class="w-full justify-start gap-2"
									onclick={() => {
										filterPopoverOpen = false;
										showSavedFiltersView = true;
									}}
								>
									<BookmarkIcon class="size-4" />
									{m.env_selector_saved_filters()}
								</Button>

								<!-- Tag Mode (only show if multiple tags selected) -->
								{#if filters.selectedTags.length > 1}
									<div class="border-border my-1 border-t"></div>
									<div class="px-2 py-1.5">
										<div class="text-muted-foreground mb-1.5 text-xs font-medium">{m.env_selector_tag_mode()}</div>
										<Tooltip.Provider>
											<div class="flex gap-1">
												<Tooltip.Root>
													<Tooltip.Trigger
														class={cn(
															'flex-1 rounded-md px-2 py-1 text-xs font-medium transition-colors',
															filters.tagMode === 'any' ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
														)}
														onclick={() => (filters = { ...filters, tagMode: 'any' })}
													>
														{m.env_selector_tag_mode_any()}
													</Tooltip.Trigger>
													<Tooltip.Content>{m.env_selector_tag_mode_any_desc()}</Tooltip.Content>
												</Tooltip.Root>
												<Tooltip.Root>
													<Tooltip.Trigger
														class={cn(
															'flex-1 rounded-md px-2 py-1 text-xs font-medium transition-colors',
															filters.tagMode === 'all' ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
														)}
														onclick={() => (filters = { ...filters, tagMode: 'all' })}
													>
														{m.env_selector_tag_mode_all()}
													</Tooltip.Trigger>
													<Tooltip.Content>{m.env_selector_tag_mode_all_desc()}</Tooltip.Content>
												</Tooltip.Root>
											</div>
										</Tooltip.Provider>
									</div>
								{/if}

								{#if hasActiveFilters || activeFilterId}
									<div class="border-border my-1 border-t"></div>
									<Button variant="ghost" size="sm" class="w-full justify-start gap-2" onclick={handleClearFilters}>
										<XIcon class="size-4" />
										{m.common_clear_filters()}
									</Button>
								{/if}

								{#if defaultFilterDisabled && savedFilters.some((f) => f.isDefault)}
									<Button variant="ghost" size="sm" class="w-full justify-start gap-2" onclick={resetToDefault}>
										<StarIcon class="size-4" />
										{m.env_selector_reset_to_default()}
									</Button>
								{/if}
							</div>
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Active Filter Pills -->
				{#if activeSavedFilter || hasAdditionalFilters || (isStatusFilterAdditional && !activeSavedFilter)}
					<div class="flex flex-wrap items-center gap-1.5">
						<!-- Saved filter badge with remove button -->
						{#if activeSavedFilter}
							<Badge variant="default" class="gap-1 pr-1">
								<BookmarkIcon class="size-3" />
								{activeSavedFilter.name}
								<button class="hover:bg-primary-foreground/20 ml-0.5 rounded p-0.5" onclick={clearSavedFilter}>
									<XIcon class="size-3" />
								</button>
							</Badge>
						{/if}

						<!-- Only show status filter if it's additional (different from saved filter or no saved filter) -->
						{#if isStatusFilterAdditional}
							<Badge variant="secondary" class="gap-1 pr-1">
								<span class={cn('size-1.5 rounded-full', getStatusColor(filters.statusFilter))}></span>
								{filters.statusFilter === 'online' ? m.common_online() : m.common_offline()}
								<button
									class="hover:bg-muted ml-0.5 rounded p-0.5"
									onclick={() => {
										// Reset to saved filter's status or 'all'
										filters = { ...filters, statusFilter: activeSavedFilter?.statusFilter ?? 'all' };
									}}
								>
									<XIcon class="size-3" />
								</button>
							</Badge>
						{/if}

						<!-- Tag mode badge (only if additional) -->
						{#if isTagModeAdditional}
							<Badge variant="secondary" class="text-[10px]">
								{filters.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
							</Badge>
						{/if}

						<!-- Only show additional selected tags (not in saved filter) -->
						{#each additionalSelectedTags as tag}
							<Badge variant="outline" class="gap-1 pr-1">
								<TagIcon class="size-3" />
								{tag}
								<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => removeTag(tag)}>
									<XIcon class="size-3" />
								</button>
							</Badge>
						{/each}

						<!-- Only show additional excluded tags (not in saved filter) -->
						{#each additionalExcludedTags as tag}
							<Badge variant="outline" class="gap-1 border-red-500/50 pr-1 text-red-600 dark:text-red-400">
								<TagIcon class="size-3" />
								{tag}
								<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => removeExcludedTag(tag)}>
									<XIcon class="size-3" />
								</button>
							</Badge>
						{/each}

						<!-- Clear additional tags button -->
						{#if additionalSelectedTags.length > 0 || additionalExcludedTags.length > 0}
							<button class="text-muted-foreground hover:text-foreground text-xs" onclick={clearTags}>
								{m.common_clear_tags()}
							</button>
						{/if}
					</div>
				{/if}

				<!-- Environment List -->
				<ScrollArea class="max-h-[45vh] min-h-[180px]">
					{#if isLoading}
						<div class="flex h-40 items-center justify-center">
							<Spinner class="size-6" />
						</div>
					{:else if environments.length === 0}
						<div class="text-muted-foreground flex h-40 flex-col items-center justify-center text-center">
							<ServerIcon class="mb-2 size-10 opacity-30" />
							<p class="text-sm font-medium">{m.common_no_results_found()}</p>
							{#if hasActiveFilters || searchQuery}
								<p class="mt-1 text-xs">{m.env_selector_try_different_filters()}</p>
								<Button variant="ghost" size="sm" class="mt-2" onclick={handleClearFilters}>
									{m.common_clear_filters()}
								</Button>
							{/if}
						</div>
					{:else if groupedEnvironments}
						<div class="space-y-2 p-1">
							{#each groupedEnvironments as group (group.name)}
								{@render envGroup(group)}
							{/each}
						</div>
					{:else}
						<div class="space-y-1 p-1">
							{#each environments as env (env.id)}
								{@render environmentItem(env)}
							{/each}
						</div>
					{/if}

					{#if hasMorePages}
						<div class="flex justify-center py-2">
							<Button variant="ghost" size="sm" onclick={loadMore} disabled={isLoading}>
								{#if isLoading}
									<Spinner class="mr-2 size-4" />
								{/if}
								{m.common_load_more()}
							</Button>
						</div>
					{/if}
				</ScrollArea>
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full items-center justify-between">
			{#if isAdmin}
				<Button
					variant="ghost"
					size="sm"
					onclick={() => {
						open = false;
						onOpenChange?.(false);
						goto('/environments');
					}}
				>
					<SettingsIcon class="mr-1.5 size-4" />
					{m.sidebar_manage_environments()}
				</Button>
			{:else}
				<div></div>
			{/if}
			<Button variant="ghost" size="sm" onclick={() => (open = false)}>{m.common_close()}</Button>
		</div>
	{/snippet}
</ResponsiveDialog>
