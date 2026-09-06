import type { ColumnFiltersState } from '@tanstack/table-core';
import type { FilterMap } from './shared';

export type PersistedPreferencesSnapshot = {
	hiddenColumns: string[];
	restoredFilters: ColumnFiltersState;
	filtersMap: FilterMap;
	search: string;
	sort?: { column: string; direction: 'asc' | 'desc' };
	limit: number;
	mobileVisibility?: string[];
	customSettings?: Record<string, unknown>;
};
