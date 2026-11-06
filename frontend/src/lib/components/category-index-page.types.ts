import type { Component } from 'svelte';

export interface CategoryMatchingItem {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

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
