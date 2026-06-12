import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { IsMobile } from './is-mobile.svelte';

const MOBILE_ROWS = 4;
const ROW_HEIGHT = 57;
const HEADER_HEIGHT = 145;
const FOOTER_HEIGHT = 48;
const MIN_ROWS = 3;
const MAX_ROWS = 50;
const DEFAULT_ROWS = 5;

type ResponsiveTableLimitOptions = {
	initialLimit?: number;
	sort: NonNullable<SearchPaginationSortRequest['sort']>;
	getTotalItems: () => number;
};

export function useResponsiveTableLimit({ initialLimit = DEFAULT_ROWS, sort, getTotalItems }: ResponsiveTableLimitOptions) {
	const isMobile = new IsMobile();
	let measuredHeight = $state(0);
	let baseRequestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: initialLimit },
		sort
	});

	function shouldReserveFooter(limit: number) {
		return getTotalItems() > limit;
	}

	function calculateLimitForHeight(height: number) {
		if (isMobile.current) return MOBILE_ROWS;
		if (height <= 0) return DEFAULT_ROWS;

		let availableHeight = height - HEADER_HEIGHT;
		const initialRows = Math.floor(Math.max(0, availableHeight) / ROW_HEIGHT);
		const footerLimit = Math.max(MIN_ROWS, Math.min(MAX_ROWS, initialRows));
		if (shouldReserveFooter(footerLimit)) {
			availableHeight -= FOOTER_HEIGHT;
		}

		const rows = Math.floor(Math.max(0, availableHeight) / ROW_HEIGHT);
		return Math.max(MIN_ROWS, Math.min(MAX_ROWS, rows));
	}

	const displayLimit = $derived(calculateLimitForHeight(measuredHeight));
	const requestOptions = $derived.by(() => {
		const limit = displayLimit;
		if (baseRequestOptions.pagination?.limit === limit) return baseRequestOptions;

		return {
			...baseRequestOptions,
			pagination: {
				page: baseRequestOptions.pagination?.page ?? 1,
				limit
			}
		};
	});

	return {
		get displayLimit() {
			return displayLimit;
		},
		get measuredHeight() {
			return measuredHeight;
		},
		set measuredHeight(value: number) {
			measuredHeight = value;
		},
		get requestOptions() {
			return requestOptions;
		},
		set requestOptions(value: SearchPaginationSortRequest) {
			baseRequestOptions = value;
		},
		shouldShowFooter(dataLength: number) {
			return dataLength >= displayLimit && getTotalItems() > displayLimit;
		}
	};
}
