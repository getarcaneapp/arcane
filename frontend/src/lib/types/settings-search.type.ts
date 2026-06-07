export interface SettingMeta {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

export interface SettingsCategory {
	id: string;
	title: string;
	description: string;
	icon: string;
	url: string;
	keywords: string[];
	settings: SettingMeta[];
	matchingSettings?: SettingMeta[];
	relevanceScore?: number;
}

export interface SettingsSearchResponse {
	results: SettingsCategory[];
	query: string;
	count: number;
}
