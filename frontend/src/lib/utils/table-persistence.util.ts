import { browser } from '$app/environment';
import { PersistedState } from 'runed';
import type { CompactTablePrefs } from '$lib/components/arcane-table/arcane-table.types.svelte';
import type { FilterMap, FilterValue, SearchPaginationSortRequest } from '$lib/types/pagination.type';

const DEFAULT_LIMIT = 20;

function cloneRequest(options: SearchPaginationSortRequest): SearchPaginationSortRequest {
	return {
		search: options.search,
		filters: options.filters ? { ...options.filters } : undefined,
		pagination: options.pagination ? { ...options.pagination } : undefined,
		sort: options.sort ? { ...options.sort } : undefined,
		includeInternal: options.includeInternal
	};
}

function buildFilterMap(pairs?: [string, unknown][]): FilterMap {
	const filters: FilterMap = {};
	if (!pairs?.length) return filters;

	for (const [id, rawValue] of pairs) {
		let value: unknown = rawValue;
		if (value instanceof Set) {
			const iterator = value.values().next();
			value = iterator.value;
		}

		if (Array.isArray(value)) {
			const first = value.find((entry) => entry !== undefined && entry !== null && `${entry}`.trim() !== '');
			if (first === undefined) continue;
			value = first;
		}

		if (value === undefined || value === null) continue;
		if (typeof value === 'string') {
			const trimmed = value.trim();
			if (!trimmed) continue;
			filters[id] = trimmed;
			continue;
		}

		filters[id] = value as FilterValue;
	}

	return filters;
}

function normalizeLimit(limit: unknown): number | undefined {
	if (typeof limit === 'number' && Number.isFinite(limit) && limit > 0) return limit;
	if (typeof limit === 'string') {
		const parsed = Number.parseInt(limit, 10);
		if (Number.isFinite(parsed) && parsed > 0) return parsed;
	}
	return undefined;
}

function normalizeSearch(value: unknown): string | undefined {
	if (typeof value !== 'string') return undefined;
	const trimmed = value.trim();
	return trimmed.length > 0 ? trimmed : undefined;
}

export function resolveInitialTableRequest(
	persistKey: string,
	defaults: SearchPaginationSortRequest
): SearchPaginationSortRequest {
	const base = cloneRequest(defaults);
	const fallbackLimit = base.pagination?.limit ?? DEFAULT_LIMIT;

	if (!base.pagination) {
		base.pagination = { page: 1, limit: fallbackLimit };
	} else {
		base.pagination = {
			page: base.pagination.page ?? 1,
			limit: base.pagination.limit ?? fallbackLimit
		};
	}

	if (!browser) return base;

	try {
		const persisted = new PersistedState<CompactTablePrefs>(
			persistKey,
			{ v: [], f: [], g: '', l: fallbackLimit },
			{ syncTabs: false }
		);
		const current = persisted.current ?? {};

		const filters = buildFilterMap(current.f);
		if (Object.keys(filters).length > 0) {
			base.filters = filters;
			base.pagination = { ...base.pagination!, page: 1 };
		}

		const search = normalizeSearch(current.g);
		if (search !== undefined) {
			base.search = search;
			base.pagination = { ...base.pagination!, page: 1 };
		}

		const limit = normalizeLimit(current.l);
		if (limit !== undefined && base.pagination?.limit !== limit) {
			base.pagination = { page: 1, limit };
		}
	} catch (error) {
		return base;
	}

	return base;
}
