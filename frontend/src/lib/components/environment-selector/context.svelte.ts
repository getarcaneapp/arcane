import { getContext, setContext } from 'svelte';
import type { EnvironmentFilter, EnvironmentFilterState, EnvSelectorStateProps, GroupBy, StatusFilter, TagMode } from './types';

/**
 * State class for the environment selector context.
 * Provides reactive access to filters and helper methods.
 */
class EnvSelectorState {
	readonly #props: EnvSelectorStateProps;

	constructor(props: EnvSelectorStateProps) {
		this.#props = props;
	}

	// Reactive getters
	get filters() {
		return this.#props.filters();
	}

	get allTags() {
		return this.#props.allTags();
	}

	get savedFilters() {
		return this.#props.savedFilters();
	}

	get activeFilterId() {
		return this.#props.activeFilterId();
	}

	get activeSavedFilter(): EnvironmentFilter | null {
		return this.savedFilters.find((f) => f.id === this.activeFilterId) ?? null;
	}

	get hasActiveFilters(): boolean {
		const f = this.filters;
		return f.selectedTags.length > 0 || f.excludedTags.length > 0 || f.statusFilter !== 'all' || f.groupBy !== 'none';
	}

	get hasSaveableFilters(): boolean {
		const f = this.filters;
		return f.selectedTags.length > 0 || f.excludedTags.length > 0 || f.statusFilter !== 'all';
	}

	get hasDefaultFilter(): boolean {
		return this.savedFilters.some((f) => f.isDefault);
	}

	// Methods
	updateFilters = (partial: Partial<EnvironmentFilterState>) => {
		this.#props.updateFilters(partial);
	};

	clearFilters = () => {
		this.#props.clearFilters();
	};

	addTag = (tag: string) => {
		if (!this.filters.selectedTags.includes(tag) && !this.filters.excludedTags.includes(tag)) {
			this.updateFilters({ selectedTags: [...this.filters.selectedTags, tag] });
		}
	};

	removeTag = (tag: string) => {
		this.updateFilters({ selectedTags: this.filters.selectedTags.filter((t) => t !== tag) });
	};

	addExcludedTag = (tag: string) => {
		if (!this.filters.selectedTags.includes(tag) && !this.filters.excludedTags.includes(tag)) {
			this.updateFilters({ excludedTags: [...this.filters.excludedTags, tag] });
		}
	};

	removeExcludedTag = (tag: string) => {
		this.updateFilters({ excludedTags: this.filters.excludedTags.filter((t) => t !== tag) });
	};

	setStatus = (status: StatusFilter) => {
		this.updateFilters({ statusFilter: status });
	};

	clearStatus = () => {
		this.updateFilters({ statusFilter: this.activeSavedFilter?.statusFilter ?? 'all' });
	};

	setGroupBy = (groupBy: GroupBy) => {
		this.updateFilters({ groupBy });
	};

	setTagMode = (tagMode: TagMode) => {
		this.updateFilters({ tagMode });
	};

	/**
	 * Check if current filters differ from a saved filter
	 */
	isFilterDifferent(savedFilter: EnvironmentFilter): boolean {
		const current = this.filters;
		return (
			JSON.stringify([...current.selectedTags].sort()) !== JSON.stringify([...savedFilter.selectedTags].sort()) ||
			JSON.stringify([...current.excludedTags].sort()) !== JSON.stringify([...savedFilter.excludedTags].sort()) ||
			current.tagMode !== savedFilter.tagMode ||
			current.statusFilter !== savedFilter.statusFilter ||
			current.groupBy !== savedFilter.groupBy
		);
	}

	/**
	 * Get tags that are additional to the active saved filter
	 */
	getAdditionalSelectedTags(): string[] {
		const filter = this.activeSavedFilter;
		return filter ? this.filters.selectedTags.filter((t) => !filter.selectedTags.includes(t)) : this.filters.selectedTags;
	}

	getAdditionalExcludedTags(): string[] {
		const filter = this.activeSavedFilter;
		return filter ? this.filters.excludedTags.filter((t) => !filter.excludedTags.includes(t)) : this.filters.excludedTags;
	}

	isStatusAdditional(): boolean {
		const filter = this.activeSavedFilter;
		return filter ? this.filters.statusFilter !== filter.statusFilter : this.filters.statusFilter !== 'all';
	}

	isTagModeAdditional(): boolean {
		const filter = this.activeSavedFilter;
		const hasMultipleTags = this.filters.selectedTags.length > 1;
		return filter
			? this.filters.tagMode !== filter.tagMode && hasMultipleTags
			: this.filters.tagMode !== 'any' && hasMultipleTags;
	}

	/**
	 * Reset tags to the active saved filter's tags or clear them
	 */
	resetTags = () => {
		const filter = this.activeSavedFilter;
		this.updateFilters(
			filter
				? { selectedTags: [...filter.selectedTags], excludedTags: [...filter.excludedTags] }
				: { selectedTags: [], excludedTags: [] }
		);
	};
}

const SYMBOL_KEY = 'env-selector';

/**
 * Create and set the environment selector context.
 * Call this in the root environment-selector component.
 */
export function setEnvSelectorContext(props: EnvSelectorStateProps): EnvSelectorState {
	return setContext(Symbol.for(SYMBOL_KEY), new EnvSelectorState(props));
}

/**
 * Get the environment selector context.
 * Call this in child components to access shared state.
 */
export function useEnvSelector(): EnvSelectorState {
	return getContext(Symbol.for(SYMBOL_KEY));
}
