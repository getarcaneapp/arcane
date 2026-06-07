export interface CustomizationMeta {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

export interface CustomizeCategory {
	id: string;
	title: string;
	description: string;
	icon: string;
	url: string;
	keywords: string[];
	customizations: CustomizationMeta[];
	matchingCustomizations?: CustomizationMeta[];
	relevanceScore?: number;
}

export interface CustomizeSearchResponse {
	results: CustomizeCategory[];
	query: string;
	count: number;
}
