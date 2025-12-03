import type { Environment, EnvironmentFilter, GroupBy, StatusFilter, TagMode } from '$lib/types/environment.type';

// Re-export types for convenience
export type { Environment, EnvironmentFilter, TagMode, StatusFilter, GroupBy };

export interface EnvironmentFilterState {
	selectedTags: string[];
	excludedTags: string[];
	tagMode: TagMode;
	statusFilter: StatusFilter;
	groupBy: GroupBy;
}

export const defaultFilterState: EnvironmentFilterState = {
	selectedTags: [],
	excludedTags: [],
	tagMode: 'any',
	statusFilter: 'all',
	groupBy: 'none'
};

export interface InputMatch {
	type: 'status' | 'include' | 'exclude';
	partial: string;
}

export interface Suggestion {
	value: string;
	label: string;
}

export interface EnvironmentGroup {
	name: string;
	items: Environment[];
}

type Getter<T> = () => T;

export interface EnvSelectorStateProps {
	filters: Getter<EnvironmentFilterState>;
	allTags: Getter<string[]>;
	savedFilters: Getter<EnvironmentFilter[]>;
	activeFilterId: Getter<string | null>;
	updateFilters: (partial: Partial<EnvironmentFilterState>) => void;
	clearFilters: () => void;
}
