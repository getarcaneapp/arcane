import type { Action } from '$lib/components/arcane-button/index.js';
import type { IconType } from '$lib/icons';

export interface ActionButtonMenuItem {
	id: string;
	label: string;
	disabled?: boolean;
	onclick?: () => void;
	href?: string;
}

export interface ActionButton {
	id: string;
	action: Action;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	onclick?: () => void;
	href?: string;
	rel?: string;
	showOnMobile?: boolean;
	badge?: string | number;
	icon?: IconType | null;
	menuItems?: ActionButtonMenuItem[];
}
