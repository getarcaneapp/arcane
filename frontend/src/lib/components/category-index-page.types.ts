import type { Component } from 'svelte';

// Normalized meta shape for a matching item within a search result.
export interface CategoryMatchingItem {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

// Normalized category / search-result shape shared by both index pages.
export interface NormalizedCategory {
	id: string;
	title: string;
	description: string;
	icon: Component;
	href: string;
	matchingItems?: CategoryMatchingItem[];
}

export interface CategoryGroup {
	id: string;
	title?: string;
	categories: NormalizedCategory[];
}
