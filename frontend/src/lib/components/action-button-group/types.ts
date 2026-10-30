import type { ResolvedPathname } from '$app/types';
import type { Snippet } from 'svelte';

import type { Action } from '#lib/components/arcane-button/index.js';
import type { IconType } from '#lib/icons/index.js';

export type ActionPlacement = 'primary' | 'secondary' | 'menu';
export type ActionGroup = 'lifecycle' | 'deploy' | 'manage' | 'danger';

export interface ActionButtonMenuItem {
	id: string;
	label: string;
	icon?: IconType;
	destructive?: boolean;
	disabled?: boolean;
	onclick?: () => void;
	href?: ResolvedPathname;
}

export interface ActionButton {
	id: string;
	action: Action;
	label: string;
	loadingLabel?: string;
	loading?: boolean;
	disabled?: boolean;
	disabledReason?: string;
	onclick?: () => void;
	href?: ResolvedPathname;
	rel?: string;
	icon?: IconType | null;
	badge?: string | number;
	placement?: ActionPlacement;
	group?: ActionGroup;
	destructive?: boolean;
	iconOnly?: boolean;
	menuItems?: ActionButtonMenuItem[];
	menuContent?: Snippet<[boolean]>;
}
