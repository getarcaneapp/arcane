import type { Action } from '$lib/components/arcane-button/index.js';
import type { IconType } from '$lib/icons';

export interface ActionButton {
	id: string;
	action: Action;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	onclick: () => void;
	showOnMobile?: boolean;
	badge?: string | number;
	icon?: IconType | null;
}
