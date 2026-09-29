import type { IconType } from '#lib/icons/index.js';

export interface SettingsStatCard {
	title: string;
	value: string | number;
	subtitle?: string;
	icon: IconType;
	iconColor?: string;
	bgColor?: string;
	class?: string;
}

export type SettingsPageType = 'form' | 'management';
