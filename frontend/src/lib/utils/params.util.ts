import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

export function transformPaginationParams(options?: SearchPaginationSortRequest): Record<string, any> {
	const params: Record<string, any> = {};

	if (!options) return params;

	if (options.search) {
		params.search = options.search;
	}

	if (options.pagination) {
		const { page, limit } = options.pagination;
		params.start = Math.max(0, (page - 1) * limit);
		params.limit = limit;
	}

	if (options.sort) {
		params.sort = options.sort.column;
		params.order = options.sort.direction;
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
		params.includeInternal = String(options.includeInternal);
	}

	return params;
}
