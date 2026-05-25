import { browser } from '$app/environment';
import { PersistedState } from 'runed';
import { decodeSort, type CompactTablePrefs } from '$lib/components/arcane-table/arcane-table.types.svelte';
import { TABLE_PAGE_SIZE_ALL, TABLE_PAGE_SIZE_OPTIONS } from '$lib/constants/table-pagination';
import type { FilterMap, FilterValue, SearchPaginationSortRequest } from '$lib/types/shared';

const DEFAULT_LIMIT = 20;

export function normalizeTablePageSize(limit: unknown): number | undefined {
	const parsed = typeof limit === 'number' ? limit : Number.parseInt(String(limit), 10);

	if (!Number.isFinite(parsed)) return undefined;
	if (parsed === TABLE_PAGE_SIZE_ALL) return parsed;
	return TABLE_PAGE_SIZE_OPTIONS.includes(parsed as (typeof TABLE_PAGE_SIZE_OPTIONS)[number]) ? parsed : undefined;
}

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

		const limit = normalizeTablePageSize(current.l);
		if (limit !== undefined && base.pagination?.limit !== limit) {
			base.pagination = { page: 1, limit };
		}

		const sort = decodeSort(current.s);
		if (sort) {
			base.sort = sort;
		}
	} catch (error) {
		return base;
	}

	return base;
}

export function transformPaginationParams(options?: SearchPaginationSortRequest): Record<string, any> {
	const params: Record<string, any> = {};

	if (!options) return params;

	if (options.search) {
		params['search'] = options.search;
	}

	if (options.pagination) {
		const { page, limit } = options.pagination;
		params['start'] = Math.max(0, (page - 1) * limit);
		params['limit'] = limit;
	}

	if (options.sort) {
		params['sort'] = options.sort.column;
		params['order'] = options.sort.direction;
	}

	if (options.filters) {
		Object.entries(options.filters).forEach(([key, value]) => {
			if (Array.isArray(value)) {
				params[key] = value.join(',');
			} else {
				params[key] = String(value);
			}
		});
	}

	if (typeof options.includeInternal === 'boolean') {
		params['includeInternal'] = String(options.includeInternal);
	}

	return params;
}
