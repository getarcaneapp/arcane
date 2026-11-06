import type { IconType } from '#lib/icons/index.js';

export interface TabItem {
	value: string;
	label: string;
	icon?: IconType;
	badge?: string | number;
	disabled?: boolean;
}
