import type { PhosphorIcon } from '$lib/types/icon.type';

export interface TabItem {
	value: string;
	label: string;
	icon?: PhosphorIcon;
	badge?: string | number;
	disabled?: boolean;
	class?: string;
}
