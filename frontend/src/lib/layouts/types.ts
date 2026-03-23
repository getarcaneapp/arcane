import type { Action } from '$lib/components/arcane-button/index.js';
import type { IconType } from '$lib/icons';

export interface SettingsActionButton {
	id: string;
	action: Action;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	onclick: () => void;
	showOnMobile?: boolean;
}

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

export interface DetailAction {
	id: string;
	action: Action;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	onclick: () => void;
}
