import type { Environment, EnvironmentFilter, TagMode, StatusFilter, GroupBy } from '$lib/types/environment.type';

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

