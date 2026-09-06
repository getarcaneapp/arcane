import type { ArcaneRow } from './table-features';
import type { RowData } from '@tanstack/table-core';
import type { ColumnFiltersState } from '@tanstack/table-core';
import type { FilterMap, FilterValue } from '#lib/types/shared.js';
import type { CompactTablePrefs } from './arcane-table.types.svelte';
import { decodeFilters, decodeSort } from './arcane-table.types.svelte';

import type { PersistedPreferencesSnapshot } from '#lib/types/table-preferences.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function toFilterMap(filters: ColumnFiltersState): FilterMap {
	const out: FilterMap = {};
	for (const f of filters ?? []) {
		const id = f.id;
		let value: unknown = f.value;

		if (value instanceof Set) {
			value = Array.from(value);
		}

		// Filter values originate from facet options / inputs, so they always fit
		// FilterValue; the cast bridges tanstack's `unknown` at this one boundary.
		if (Array.isArray(value)) {
			if (value.length === 0) continue;
			out[id] = value as FilterValue;
		} else if (value !== undefined && value !== null && String(value).trim() !== '') {
			out[id] = value as FilterValue;
		}
	}
	return out;
}

/**
 * Inverse of {@link toFilterMap}: rebuilds tanstack `ColumnFiltersState` from a
 * request `FilterMap`. Used to reflect externally-set `requestOptions.filters`
 * (e.g. a clickable stat card) back into the faceted-filter UI. Values are kept
 * as-is so they match the facet option `value` types (array, boolean, string).
 */
export function fromFilterMap(map?: FilterMap): ColumnFiltersState {
	if (!map) return [];
	const out: ColumnFiltersState = [];
	for (const [id, value] of Object.entries(map)) {
		if (value === undefined || value === null) continue;
		if (Array.isArray(value) && value.length === 0) continue;
		out.push({ id, value });
	}
	return out;
}

function filterValuesEqual(a: unknown, b: unknown): boolean {
	if (Array.isArray(a) || Array.isArray(b)) {
		return (
			Array.isArray(a) &&
			Array.isArray(b) &&
			a.length === b.length &&
			Array.from(a).every((value, index) => `${value}` === `${b[index]}`)
		);
	}
	if (a === b) return true;
	return a != null && b != null && `${a}` === `${b}`;
}

export function filterMapsEqual(a?: FilterMap, b?: FilterMap): boolean {
	const keys = Object.keys(a ?? {});
	return keys.length === Object.keys(b ?? {}).length && keys.every((key) => filterValuesEqual(a?.[key], b?.[key]));
}

export function restoreTableRequestOptions(
	current: SearchPaginationSortRequest,
	snapshot: PersistedPreferencesSnapshot,
	fallbackLimit: number
): SearchPaginationSortRequest {
	let next = current;
	function apply(patch: Partial<SearchPaginationSortRequest>) {
		next = { ...next, pagination: { page: 1, limit: next?.pagination?.limit ?? fallbackLimit }, ...patch };
	}

	const filters = Object.keys(snapshot.filtersMap).length ? snapshot.filtersMap : undefined;
	if (!filterMapsEqual(filters, current?.filters)) apply({ filters });

	const currentSearch = (current?.search ?? '').trim();
	const search = currentSearch || snapshot.search;
	if (search !== currentSearch) apply({ search: search || undefined });

	if (snapshot.limit !== (next?.pagination?.limit ?? fallbackLimit)) {
		apply({ pagination: { page: 1, limit: snapshot.limit } });
	}

	const sort = snapshot.sort;
	if (sort && (next?.sort?.column !== sort.column || next?.sort?.direction !== sort.direction)) apply({ sort });
	return next;
}

export function extractPersistedPreferences(
	current: CompactTablePrefs | undefined,
	fallbackLimit: number
): PersistedPreferencesSnapshot {
	const prefs = current ?? { v: [], f: [], g: '', l: fallbackLimit };
	const restoredFilters = decodeFilters(prefs.f);
	const filtersMap = toFilterMap(restoredFilters);
	const search = (prefs.g ?? '').trim();
	const sort = decodeSort(prefs.s);
	const limit = prefs.l ?? fallbackLimit;

	return {
		hiddenColumns: prefs.v ?? [],
		restoredFilters,
		filtersMap,
		search,
		sort,
		limit,
		mobileVisibility: prefs.m,
		customSettings: prefs.c
	};
}

export function getTableRowsForItems<T extends RowData>(
	rowIndex: ReadonlyMap<string, { row: ArcaneRow<T>; index: number }>,
	groupItems: Array<{ id: string }>
): ArcaneRow<T>[] {
	const ids = new Set(groupItems.map((item) => item.id));
	return Array.from(ids)
		.flatMap((id) => {
			const entry = rowIndex.get(id);
			return entry ? [entry] : [];
		})
		.sort((a, b) => a.index - b.index)
		.map((entry) => entry.row);
}
