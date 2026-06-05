import { debounced } from '$lib/utils/ws';

type CategorySearchResponse<T> = {
	results?: T[];
};

type UseCategorySearchOptions<T> = {
	search: (query: string) => Promise<CategorySearchResponse<T>>;
	filter: (category: T) => boolean;
	onError?: (error: unknown) => void;
};

export function useCategorySearch<T>({ search, filter, onError }: UseCategorySearchOptions<T>) {
	let searchQuery = $state('');
	let showSearchResults = $state(false);
	let searchResults = $state<T[]>([]);
	let isSearching = $state(false);
	let currentSearchRequest = 0;

	async function performSearch(query: string) {
		const trimmedQuery = query.trim();

		if (!trimmedQuery) {
			searchResults = [];
			showSearchResults = false;
			isSearching = false;
			currentSearchRequest++;
			return;
		}

		currentSearchRequest++;
		const requestId = currentSearchRequest;
		isSearching = true;
		showSearchResults = true;

		try {
			const response = await search(trimmedQuery);
			if (requestId === currentSearchRequest) {
				searchResults = (response.results || []).filter(filter);
				isSearching = false;
			}
		} catch (error) {
			onError?.(error);
			if (requestId === currentSearchRequest) {
				searchResults = [];
				isSearching = false;
			}
		}
	}

	const debouncedSearch = debounced((query: string) => {
		void performSearch(query);
	}, 300);

	function clearSearch() {
		searchQuery = '';
		showSearchResults = false;
		isSearching = false;
		searchResults = [];
		currentSearchRequest++;
	}

	return {
		get searchQuery() {
			return searchQuery;
		},
		set searchQuery(value: string) {
			searchQuery = value;
		},
		get showSearchResults() {
			return showSearchResults;
		},
		get searchResults() {
			return searchResults;
		},
		get isSearching() {
			return isSearching;
		},
		performSearch,
		debouncedSearch,
		clearSearch
	};
}
