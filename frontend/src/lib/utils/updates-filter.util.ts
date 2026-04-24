import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

export const updatesFilter = 'has_update';

export function ensureUpdatesFilter<T extends SearchPaginationSortRequest>(options: T): T {
	return {
		...options,
		filters: {
			...(options.filters ?? {}),
			updates: updatesFilter
		}
	};
}

export function ensureStandaloneContainerUpdatesFilter<T extends SearchPaginationSortRequest>(options: T): T {
	const next = ensureUpdatesFilter(options);
	return {
		...next,
		filters: {
			...(next.filters ?? {}),
			standalone: true
		}
	};
}
