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
	includeInternal?: boolean;
	includeHidden?: boolean;
};

export type CreateRequest<T, W extends keyof T, R extends W = never> = Pick<T, R> & Partial<Pick<T, Exclude<W, R>>>;

export type UpdateRequest<T, W extends keyof T> = Partial<Pick<T, W>>;
