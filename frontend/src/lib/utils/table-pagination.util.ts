import { TABLE_PAGE_SIZE_ALL, TABLE_PAGE_SIZE_OPTIONS } from '$lib/constants/table-pagination';

export function normalizeTablePageSize(limit: unknown): number | undefined {
	const parsed = typeof limit === 'number' ? limit : Number.parseInt(String(limit), 10);

	if (!Number.isFinite(parsed)) return undefined;
	if (parsed === TABLE_PAGE_SIZE_ALL) return parsed;
	return TABLE_PAGE_SIZE_OPTIONS.includes(parsed as (typeof TABLE_PAGE_SIZE_OPTIONS)[number]) ? parsed : undefined;
}
