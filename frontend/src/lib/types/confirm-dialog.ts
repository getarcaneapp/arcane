import type { Action } from '#lib/components/arcane-button/index.js';

export interface ConfirmDialogOptions {
	title: string;
	message: string;
	confirm: {
		label?: string;
		destructive?: boolean;
		/** ArcaneButton action variant for the confirm button; overrides the destructive/remove default. */
		button?: Action;
		action: (checkboxStates: Record<string, boolean>) => void | Promise<void>;
	};
	checkboxes?: Array<{
		id: string;
		label: string;
		initialState?: boolean;
	}>;
}

export type ConfirmDialogState = {
	current: (ConfirmDialogOptions & { open: boolean; checkboxStates: Record<string, boolean> }) | null;
};
