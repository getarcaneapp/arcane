import type { Action } from '#lib/components/arcane-button/index.js';
import type { IconType } from '#lib/icons/index.js';
import type { Snippet } from 'svelte';

export type ActionPlacement = 'primary' | 'secondary' | 'menu';
export type ActionGroup = 'lifecycle' | 'deploy' | 'manage' | 'danger';

export interface ActionButtonMenuItem {
	id: string;
	label: string;
	icon?: IconType;
	destructive?: boolean;
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
	disabledReason?: string;
	onclick?: () => void;
	href?: string;
	rel?: string;
	icon?: IconType | null;
	badge?: string | number;
	// Inline placement; defaults to the Actions menu.
	placement?: ActionPlacement;
	// Menu section; defaults to "manage".
	group?: ActionGroup;
	destructive?: boolean;
	// Inline icon button with a tooltip instead of a label.
	iconOnly?: boolean;
	menuItems?: ActionButtonMenuItem[];
	// Rich dropdown entries rendered after menuItems; receives the parent's disabled state.
	menuContent?: Snippet<[boolean]>;
}
