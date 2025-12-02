import { PersistedState } from 'runed';

export type TagMode = 'any' | 'all';
export type StatusFilter = 'all' | 'online' | 'offline';
export type GroupBy = 'none' | 'status' | 'tags';

interface EnvSelectorFilterState {
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

function createEnvSelectorFilterStore() {
	const persisted = new PersistedState<EnvSelectorFilterState>('envSelectorFilter', defaultState);

	return {
		get state() {
			return persisted.current;
		},
		get selectedTags() {
			return persisted.current.selectedTags;
		},
		set selectedTags(value: string[]) {
			persisted.current = { ...persisted.current, selectedTags: value };
		},
		get excludedTags() {
			return persisted.current.excludedTags;
		},
		set excludedTags(value: string[]) {
			persisted.current = { ...persisted.current, excludedTags: value };
		},
		get tagMode() {
			return persisted.current.tagMode;
		},
		set tagMode(value: TagMode) {
			persisted.current = { ...persisted.current, tagMode: value };
		},
		get statusFilter() {
			return persisted.current.statusFilter;
		},
		set statusFilter(value: StatusFilter) {
			persisted.current = { ...persisted.current, statusFilter: value };
		},
		get groupBy() {
			return persisted.current.groupBy;
		},
		set groupBy(value: GroupBy) {
			persisted.current = { ...persisted.current, groupBy: value };
		},
		addTag(tag: string) {
			if (!persisted.current.selectedTags.includes(tag)) {
				persisted.current = {
					...persisted.current,
					selectedTags: [...persisted.current.selectedTags, tag]
				};
			}
		},
		removeTag(tag: string) {
			persisted.current = {
				...persisted.current,
				selectedTags: persisted.current.selectedTags.filter((t) => t !== tag)
			};
		},
		addExcludedTag(tag: string) {
			if (!persisted.current.excludedTags.includes(tag)) {
				persisted.current = {
					...persisted.current,
					excludedTags: [...persisted.current.excludedTags, tag]
				};
			}
		},
		removeExcludedTag(tag: string) {
			persisted.current = {
				...persisted.current,
				excludedTags: persisted.current.excludedTags.filter((t) => t !== tag)
			};
		},
		clearTags() {
			persisted.current = {
				...persisted.current,
				selectedTags: [],
				excludedTags: []
			};
		},
		clearFilters() {
			persisted.current = defaultState;
		}
	};
}

export const envSelectorFilterStore = createEnvSelectorFilterStore();

