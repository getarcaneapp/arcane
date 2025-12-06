import { getContext, setContext } from 'svelte';
import type { EnvironmentFilter } from '$lib/types/environment.type';
import type { EnvironmentFilterState } from './types';

interface EnvSelectorProps {
	filters: () => EnvironmentFilterState;
	allTags: () => string[];
	savedFilters: () => EnvironmentFilter[];
	activeFilterId: () => string | null;
	updateFilters: (partial: Partial<EnvironmentFilterState>) => void;
	clearFilters: () => void;
}

class EnvSelectorState {
	readonly #props: EnvSelectorProps;

	constructor(props: EnvSelectorProps) {
		this.#props = props;
	}

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
	get hasDefaultFilter() {
		return this.savedFilters.some((f) => f.isDefault);
	}
	get hasActiveFilters() {
		const f = this.filters;
		return f.selectedTags.length > 0 || f.excludedTags.length > 0 || f.statusFilter !== 'all' || f.groupBy !== 'none';
	}
	get hasSaveableFilters() {
		const f = this.filters;
		return f.selectedTags.length > 0 || f.excludedTags.length > 0 || f.statusFilter !== 'all';
	}

	updateFilters = (partial: Partial<EnvironmentFilterState>) => this.#props.updateFilters(partial);
	clearFilters = () => this.#props.clearFilters();

	// Tag operations
	addTag = (tag: string) => {
		const { selectedTags, excludedTags } = this.filters;
		if (!selectedTags.includes(tag) && !excludedTags.includes(tag)) {
			this.updateFilters({ selectedTags: [...selectedTags, tag] });
		}
	};
	removeTag = (tag: string) => this.updateFilters({ selectedTags: this.filters.selectedTags.filter((t) => t !== tag) });
	addExcludedTag = (tag: string) => {
		const { selectedTags, excludedTags } = this.filters;
		if (!selectedTags.includes(tag) && !excludedTags.includes(tag)) {
			this.updateFilters({ excludedTags: [...excludedTags, tag] });
		}
	};
	removeExcludedTag = (tag: string) => this.updateFilters({ excludedTags: this.filters.excludedTags.filter((t) => t !== tag) });

	// Filter operations
	setStatus = (status: 'all' | 'online' | 'offline') => this.updateFilters({ statusFilter: status });
	setGroupBy = (groupBy: 'none' | 'status' | 'tags') => this.updateFilters({ groupBy });
	setTagMode = (tagMode: 'any' | 'all') => this.updateFilters({ tagMode });

	// Check if current filters differ from a saved filter
	isFilterDifferent(filter: EnvironmentFilter): boolean {
		const c = this.filters;
		return (
			JSON.stringify([...c.selectedTags].sort()) !== JSON.stringify([...filter.selectedTags].sort()) ||
			JSON.stringify([...c.excludedTags].sort()) !== JSON.stringify([...filter.excludedTags].sort()) ||
			c.tagMode !== filter.tagMode ||
			c.statusFilter !== filter.statusFilter ||
			c.groupBy !== filter.groupBy
		);
	}
}

const KEY = Symbol.for('env-selector');

export function setEnvSelectorContext(props: EnvSelectorProps): EnvSelectorState {
	return setContext(KEY, new EnvSelectorState(props));
}

export function useEnvSelector(): EnvSelectorState {
	return getContext(KEY);
}
