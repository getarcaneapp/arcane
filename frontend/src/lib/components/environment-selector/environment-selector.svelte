<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import type { Environment, EnvironmentFilter } from '$lib/types/environment.type';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { debounced } from '$lib/utils/utils';
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';
	import type { PaginationResponse } from '$lib/types/pagination.type';
	import { type EnvironmentFilterState, type InputMatch, type Suggestion, defaultFilterState } from './types';
	import { setEnvSelectorContext } from './context.svelte';
	import EnvironmentList from './environment-list.svelte';
	import SavedFiltersView from './saved-filters-view.svelte';
	import FilterToolbar from './filter-toolbar.svelte';
	import FilterChips from './filter-chips.svelte';

	interface Props {
		open?: boolean;
		isAdmin?: boolean;
		onOpenChange?: (open: boolean) => void;
		trigger?: import('svelte').Snippet;
	}

	let { open = $bindable(false), isAdmin = false, onOpenChange, trigger }: Props = $props();

	// Core state
	let environments = $state.raw<Environment[]>([]);
	let pagination = $state<PaginationResponse | null>(null);
	let allTags = $state.raw<string[]>([]);
	let savedFilters = $state.raw<EnvironmentFilter[]>([]);
	let filters = $state<EnvironmentFilterState>({ ...defaultFilterState });
	let activeFilterId = $state<string | null>(null);

	// UI state
	let isLoading = $state(true);
	let inputValue = $state('');
	let defaultFilterDisabled = $state(false);
	let selectedSuggestionIndex = $state(0);
	let showSavedFiltersView = $state(false);

	// Context for child components
	setEnvSelectorContext({
		filters: () => filters,
		allTags: () => allTags,
		savedFilters: () => savedFilters,
		activeFilterId: () => activeFilterId,
		updateFilters: (partial) => (filters = { ...filters, ...partial }),
		clearFilters: () => {
			filters = { ...defaultFilterState };
			activeFilterId = null;
			defaultFilterDisabled = true;
		}
	});

	// Extract search query from input (removes tag: and is: commands)
	const searchQuery = $derived(
		inputValue
			.replace(/-?tag:\S*/gi, '')
			.replace(/is:\S*/gi, '')
			.trim()
	);

	// Parse input for autocomplete
	const inputMatch = $derived.by((): InputMatch | null => {
		const patterns = [
			{ regex: /is:(\S*)$/i, type: 'status' as const },
			{ regex: /-tag:(\S*)$/i, type: 'exclude' as const },
			{ regex: /tag:(\S*)$/i, type: 'include' as const }
		];
		for (const { regex, type } of patterns) {
			const match = inputValue.match(regex);
			if (match) return { type, partial: match[1].toLowerCase() };
		}
		return null;
	});

	// Generate autocomplete suggestions
	const suggestions = $derived.by((): Suggestion[] => {
		if (!inputMatch) return [];
		if (inputMatch.type === 'status') {
			return [
				{ value: 'online', label: m.common_online() },
				{ value: 'offline', label: m.common_offline() }
			].filter(
				(s) =>
					(s.value.includes(inputMatch.partial) || s.label.toLowerCase().includes(inputMatch.partial)) &&
					filters.statusFilter !== s.value
			);
		}
		const usedTags = new Set([...filters.selectedTags, ...filters.excludedTags]);
		return allTags
			.filter((tag) => !usedTags.has(tag) && tag.toLowerCase().includes(inputMatch.partial))
			.map((tag) => ({ value: tag, label: tag }));
	});

	// Group environments for display
	const groupedEnvironments = $derived.by(() => {
		if (filters.groupBy === 'none') return null;
		const groups = new Map<string, Environment[]>();
		for (const env of environments) {
			const keys = filters.groupBy === 'status' ? [env.status] : env.tags?.length ? env.tags : [m.common_none()];
			for (const key of keys) {
				const existing = groups.get(key) ?? [];
				if (!existing.some((e) => e.id === env.id)) existing.push(env);
				groups.set(key, existing);
			}
		}
		return [...groups.entries()]
			.map(([name, items]) => ({ name, items }))
			.sort((a, b) => (a.name === 'online' ? -1 : b.name === 'online' ? 1 : a.name.localeCompare(b.name)));
	});

	// Derived state
	const hasActiveFilters = $derived(
		filters.selectedTags.length > 0 ||
			filters.excludedTags.length > 0 ||
			filters.statusFilter !== 'all' ||
			filters.groupBy !== 'none'
	);
	const hasMorePages = $derived(pagination ? pagination.currentPage < pagination.totalPages : false);
	const filterKey = $derived(
		JSON.stringify([searchQuery, filters.statusFilter, filters.selectedTags, filters.excludedTags, filters.tagMode])
	);

	// Effects
	$effect(() => {
		selectedSuggestionIndex = suggestions.length > 0 ? 0 : -1;
	});

	$effect(() => {
		// Auto-reset invalid groupBy
		const canGroupByStatus = filters.statusFilter === 'all';
		const canGroupByTags = allTags.length > 0 && filters.selectedTags.length === 0 && filters.excludedTags.length === 0;
		if ((filters.groupBy === 'status' && !canGroupByStatus) || (filters.groupBy === 'tags' && !canGroupByTags)) {
			filters = { ...filters, groupBy: 'none' };
		}
	});

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

	// Debounced reload on filter changes
	const debouncedLoad = debounced(() => open && loadEnvironments(), 300);
	let lastFilterKey = '';
	$effect(() => {
		const key = filterKey;
		if (open && lastFilterKey && lastFilterKey !== key) {
			untrack(() => debouncedLoad());
		}
		lastFilterKey = key;
	});

	// Data loading
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
			if (!activeFilterId && !defaultFilterDisabled) {
				const defaultFilter = savedFilters.find((f) => f.isDefault);
				if (defaultFilter) applyFilter(defaultFilter);
			}
		} catch (error) {
			console.error('Failed to load saved filters:', error);
		}
	}

	// Filter operations
	function applyFilter(filter: EnvironmentFilter) {
		filters = {
			searchQuery: filter.searchQuery ?? '',
			selectedTags: filter.selectedTags ?? [],
			excludedTags: filter.excludedTags ?? [],
			tagMode: filter.tagMode,
			statusFilter: filter.statusFilter,
			groupBy: filter.groupBy
		};
		inputValue = filter.searchQuery ?? '';
		activeFilterId = filter.id;
		showSavedFiltersView = false;
	}

	function clearFilters() {
		filters = { ...defaultFilterState };
		activeFilterId = null;
		defaultFilterDisabled = true;
	}

	function resetToDefault() {
		defaultFilterDisabled = false;
		const defaultFilter = savedFilters.find((f) => f.isDefault);
		defaultFilter ? applyFilter(defaultFilter) : clearFilters();
		inputValue = '';
		defaultFilterDisabled = !defaultFilter;
	}

	// Input handling
	function handleKeydown(event: KeyboardEvent) {
		if (!suggestions.length) return;
		const actions: Record<string, () => void> = {
			ArrowDown: () => (selectedSuggestionIndex = Math.min(selectedSuggestionIndex + 1, suggestions.length - 1)),
			ArrowUp: () => (selectedSuggestionIndex = Math.max(selectedSuggestionIndex - 1, 0)),
			Tab: () => selectSuggestion(selectedSuggestionIndex),
			Enter: () => selectSuggestion(selectedSuggestionIndex),
			Escape: () => (inputValue = searchQuery)
		};
		if (actions[event.key]) {
			event.preventDefault();
			actions[event.key]();
		}
	}

	function selectSuggestion(index: number) {
		const suggestion = suggestions[index];
		if (!suggestion || !inputMatch) return;

		if (inputMatch.type === 'status') {
			filters = { ...filters, statusFilter: suggestion.value as 'online' | 'offline' };
		} else {
			const key = inputMatch.type === 'exclude' ? 'excludedTags' : 'selectedTags';
			const other = inputMatch.type === 'exclude' ? 'selectedTags' : 'excludedTags';
			if (!filters[key].includes(suggestion.value) && !filters[other].includes(suggestion.value)) {
				filters = { ...filters, [key]: [...filters[key], suggestion.value] };
			}
		}
		inputValue = searchQuery;
	}

	// Environment selection
	async function handleSelectEnvironment(env: Environment) {
		if (!env.enabled) return toast.error(m.environments_cannot_switch_disabled());
		try {
			await environmentStore.setEnvironment(env);
			toast.success(m.environments_switched_to({ name: env.name }));
			open = false;
			onOpenChange?.(false);
		} catch {
			toast.error(m.env_selector_switch_failed());
		}
	}

	// Saved filter CRUD helpers
	async function withToast<T>(fn: () => Promise<T>, successMsg: string, errorMsg: string): Promise<T | null> {
		try {
			const result = await fn();
			toast.success(successMsg);
			return result;
		} catch (error) {
			console.error(errorMsg, error);
			toast.error(errorMsg);
			return null;
		}
	}

	async function handleSaveFilter(name: string) {
		const filterData = { ...filters, searchQuery };
		const created = await withToast(
			() => environmentManagementService.createSavedFilter({ name, ...filterData }),
			m.common_create_success({ resource: m.common_filter() }),
			m.common_create_failed({ resource: m.common_filter() })
		);
		if (created) {
			savedFilters = [...savedFilters, created].sort((a, b) => a.name.localeCompare(b.name));
			activeFilterId = created.id;
		}
	}

	async function handleUpdateFilter(filterId: string) {
		const filterData = { ...filters, searchQuery };
		const updated = await withToast(
			() => environmentManagementService.updateSavedFilter(filterId, filterData),
			m.common_update_success({ resource: m.common_filter() }),
			m.common_update_failed({ resource: m.common_filter() })
		);
		if (updated) savedFilters = savedFilters.map((f) => (f.id === filterId ? updated : f));
	}

	async function handleDeleteFilter(filterId: string) {
		const success = await withToast(
			() => environmentManagementService.deleteSavedFilter(filterId),
			m.common_delete_success({ resource: m.common_filter() }),
			m.common_delete_failed({ resource: m.common_filter() })
		);
		if (success !== null) {
			savedFilters = savedFilters.filter((f) => f.id !== filterId);
			if (activeFilterId === filterId) activeFilterId = null;
		}
	}

	async function handleSetFilterDefault(filterId: string) {
		const updated = await withToast(
			() => environmentManagementService.updateSavedFilter(filterId, { isDefault: true }),
			m.common_update_success({ resource: m.common_filter() }),
			m.common_update_failed({ resource: m.common_filter() })
		);
		if (updated) {
			savedFilters = savedFilters.map((f) => ({ ...f, isDefault: f.id === filterId }));
			defaultFilterDisabled = false;
		}
	}

	async function handleClearFilterDefault() {
		const currentDefault = savedFilters.find((f) => f.isDefault);
		if (!currentDefault) return;
		const updated = await withToast(
			() => environmentManagementService.updateSavedFilter(currentDefault.id, { isDefault: false }),
			m.common_update_success({ resource: m.common_filter() }),
			m.common_update_failed({ resource: m.common_filter() })
		);
		if (updated) savedFilters = savedFilters.map((f) => ({ ...f, isDefault: false }));
	}

	async function handleRenameFilter(filterId: string, name: string) {
		const updated = await withToast(
			() => environmentManagementService.updateSavedFilter(filterId, { name }),
			m.common_update_success({ resource: m.common_filter() }),
			m.common_update_failed({ resource: m.common_filter() })
		);
		if (updated) savedFilters = savedFilters.map((f) => (f.id === filterId ? updated : f));
	}
</script>

<ResponsiveDialog bind:open {onOpenChange} {trigger} title={m.env_selector_title()} contentClass="sm:max-w-2xl">
	{#snippet children()}
		{#if showSavedFiltersView}
			<SavedFiltersView
				onBack={() => (showSavedFiltersView = false)}
				onApplyFilter={applyFilter}
				onSaveFilter={handleSaveFilter}
				onUpdateFilter={handleUpdateFilter}
				onDeleteFilter={handleDeleteFilter}
				onSetDefault={handleSetFilterDefault}
				onClearDefault={handleClearFilterDefault}
				onRenameFilter={handleRenameFilter}
			/>
		{:else}
			<div class="flex flex-col gap-3 pt-2">
				<FilterToolbar
					bind:inputValue
					{suggestions}
					{inputMatch}
					bind:selectedSuggestionIndex
					{defaultFilterDisabled}
					onKeydown={handleKeydown}
					onSelectSuggestion={selectSuggestion}
					onClearFilters={clearFilters}
					onResetToDefault={resetToDefault}
					onShowSavedFilters={() => (showSavedFiltersView = true)}
				/>

				<FilterChips
					onClearSavedFilter={() => {
						activeFilterId = null;
						defaultFilterDisabled = true;
					}}
				/>

				<EnvironmentList
					{environments}
					{groupedEnvironments}
					selectedEnvId={environmentStore.selected?.id}
					{isLoading}
					{hasActiveFilters}
					{searchQuery}
					{hasMorePages}
					onSelect={handleSelectEnvironment}
					onClearFilters={() => {
						inputValue = '';
						clearFilters();
					}}
					onLoadMore={() => hasMorePages && !isLoading && loadEnvironments((pagination?.currentPage ?? 0) + 1)}
				/>
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
