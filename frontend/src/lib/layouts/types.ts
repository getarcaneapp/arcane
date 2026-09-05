import type { Action, ArcaneButtonSize } from '#lib/components/arcane-button/index.js';
import type { IconType } from '#lib/icons';

export interface SettingsActionOption {
	label: string;
	icon?: IconType;
	onclick: () => void;
	disabled?: boolean;
}

export interface SettingsActionButton {
	id: string;
	action: Action;
	icon?: IconType;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	size?: ArcaneButtonSize;
	onclick?: () => void;
	// When set, the button renders as a dropdown trigger and onclick is unused.
	options?: SettingsActionOption[];
	showOnMobile?: boolean;
	primary?: boolean;
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
