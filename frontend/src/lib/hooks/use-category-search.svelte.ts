import { extractApiErrorMessage } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { debounced } from '#lib/utils/ws.js';

type CategorySearchResponse<T> = {
	results?: T[];
};

type UseCategorySearchOptions<T> = {
	search: (query: string) => Promise<CategorySearchResponse<T>>;
	filter: (category: T) => boolean;
};

export function useCategorySearch<T>({ search, filter }: UseCategorySearchOptions<T>) {
	let searchQuery = $state('');
	let showSearchResults = $state(false);
	let searchResults = $state<T[]>([]);
	let isSearching = $state(false);
	let searchError = $state<string | null>(null);
	let currentSearchRequest = 0;

	async function searchMatches(query: string) {
		const response = await search(query);
		return (response.results || []).filter(filter);
	}

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
		searchError = null;

		const matches = await tryCatch(searchMatches(trimmedQuery));
		if (requestId !== currentSearchRequest) return;
		if (matches.error !== null) {
			searchError = extractApiErrorMessage(matches.error);
			searchResults = [];
			isSearching = false;
			return;
		}
		searchResults = matches.data;
		isSearching = false;
	}

	const debouncedSearch = debounced((query: string) => {
		void performSearch(query);
	}, 300);

	function clearSearch() {
		searchQuery = '';
		showSearchResults = false;
		isSearching = false;
		searchError = null;
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
		get searchError() {
			return searchError;
		},
		performSearch,
		debouncedSearch,
		clearSearch
	};
}
