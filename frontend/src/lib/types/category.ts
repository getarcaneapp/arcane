import type { SettingMetadata } from './meta';

export interface Category {
	id: string;
	title: string;
	description: string;
	icon: string;
	url: string;
	keywords: string[];
	settings: SettingMetadata[];
	matchingSettings?: SettingMetadata[];
	relevanceScore?: number;
}
