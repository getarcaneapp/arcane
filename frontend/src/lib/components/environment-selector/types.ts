import type { Environment, EnvironmentFilter, GroupBy, StatusFilter, TagMode } from '$lib/types/environment.type';

export type { Environment, EnvironmentFilter, TagMode, StatusFilter, GroupBy };

export interface EnvironmentFilterState {
	searchQuery: string;
	selectedTags: string[];
	excludedTags: string[];
	tagMode: TagMode;
	statusFilter: StatusFilter;
	groupBy: GroupBy;
}

export const defaultFilterState: EnvironmentFilterState = {
	searchQuery: '',
	selectedTags: [],
	excludedTags: [],
	tagMode: 'any',
	statusFilter: 'all',
	groupBy: 'none'
};

export interface EnvironmentGroup {
	name: string;
	items: Environment[];
}

export interface InputMatch {
	type: 'status' | 'include' | 'exclude';
	partial: string;
}

export interface Suggestion {
	value: string;
	label: string;
}
