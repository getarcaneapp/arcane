export const TABLE_PAGE_SIZE_ALL = -1;
export const TABLE_PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50, 100] as const;

/** Bulk reads walk a list in pages of this size instead of one unbounded request. */
export const BULK_FETCH_PAGE_SIZE = 100;
/** Bulk reads stop after this many rows; keep it a multiple of the page size. */
export const BULK_FETCH_MAX_ITEMS = 5000;
