import { PersistedState } from 'runed';

export type TagMode = 'any' | 'all';
export type StatusFilter = 'all' | 'online' | 'offline';
export type GroupBy = 'none' | 'status' | 'tags';

export interface EnvSelectorFilterState {
	selectedTags: string[];
	excludedTags: string[];
	tagMode: TagMode;
	statusFilter: StatusFilter;
	groupBy: GroupBy;
}

const defaultState: EnvSelectorFilterState = {
	selectedTags: [],
	excludedTags: [],
	tagMode: 'any',
	statusFilter: 'all',
	groupBy: 'none'
};

export const envSelectorFilterStore = new PersistedState<EnvSelectorFilterState>(
	'envSelectorFilter',
	defaultState
);

export function addTag(tag: string) {
	const current = envSelectorFilterStore.current;
	if (!current.selectedTags.includes(tag)) {
		envSelectorFilterStore.current = {
			...current,
			selectedTags: [...current.selectedTags, tag]
		};
	}
}

export function removeTag(tag: string) {
	const current = envSelectorFilterStore.current;
	envSelectorFilterStore.current = {
		...current,
		selectedTags: current.selectedTags.filter((t) => t !== tag)
	};
}

export function addExcludedTag(tag: string) {
	const current = envSelectorFilterStore.current;
	if (!current.excludedTags.includes(tag)) {
		envSelectorFilterStore.current = {
			...current,
			excludedTags: [...current.excludedTags, tag]
		};
	}
}

export function removeExcludedTag(tag: string) {
	const current = envSelectorFilterStore.current;
	envSelectorFilterStore.current = {
		...current,
		excludedTags: current.excludedTags.filter((t) => t !== tag)
	};
}

export function clearTags() {
	const current = envSelectorFilterStore.current;
	envSelectorFilterStore.current = {
		...current,
		selectedTags: [],
		excludedTags: []
	};
}

export function clearFilters() {
	envSelectorFilterStore.current = defaultState;
}
