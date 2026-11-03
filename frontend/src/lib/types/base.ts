export type MessageResponse = {
	activityId?: string;
	message: string;
};

export type PaginationResponse = {
	totalPages: number;
	totalItems: number;
	currentPage: number;
	itemsPerPage: number;
	grandTotalItems?: number;
	hasNextPage?: boolean;
};

export type Paginated<T> = {
	data: T[];
	pagination: PaginationResponse;
};

export type PaginatedWithWarnings<T> = Paginated<T> & {
	warnings?: string[];
};

export type PaginatedWithCounts<T, C> = Paginated<T> & {
	counts: C;
};
