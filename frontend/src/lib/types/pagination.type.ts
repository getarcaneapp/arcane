export type PaginationRequest = {
	page: number;
	limit: number;
};

export type SortRequest = {
	column: string;
	direction: 'asc' | 'desc';
};

export type FilterValue = string | number | boolean | (string | number | boolean)[];
export type FilterMap = Record<string, FilterValue>;

export type SearchPaginationSortRequest = {
	search?: string;
	pagination?: PaginationRequest;
	sort?: SortRequest;
	filters?: FilterMap;
};

export type PaginationResponse = {
	totalPages: number;
	totalItems: number;
	currentPage: number;
	itemsPerPage: number;
	grandTotalItems?: number;
};

export type Paginated<T, C = unknown> = {
	data: T[];
	pagination: PaginationResponse;
	counts?: C;
};

// export interface PaginatedApiResponse<T> {
// 	success: boolean;
// 	data: T[];
// 	pagination: PaginationResponse;
// }

// export interface SortedPaginationRequest {
// 	pagination: {
// 		page: number;
// 		limit: number;
// 	};
// 	sort: {
// 		column: string;
// 		direction: 'asc' | 'desc';
// 	};
// }
