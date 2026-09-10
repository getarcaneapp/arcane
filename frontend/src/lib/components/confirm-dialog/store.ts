import { writable } from 'svelte/store';
import { m } from '#lib/paraglide/messages.js';
import type { ConfirmDialogOptions } from '#lib/types/confirm-dialog.js';

export const confirmDialogStore = writable<ConfirmDialogOptions & { open: boolean; checkboxStates: Record<string, boolean> }>({
	open: false,
	checkboxStates: {},
	title: '',
	message: '',
	confirm: {
		label: m.common_confirm(),
		destructive: false,
		action: () => {}
	}
});

export function openConfirmDialog({ title, message, confirm, checkboxes }: ConfirmDialogOptions) {
	confirmDialogStore.update(() => ({
		open: true,
		checkboxStates: Object.fromEntries((checkboxes ?? []).map((checkbox) => [checkbox.id, Boolean(checkbox.initialState)])),
		title,
		message,
		confirm: {
			label: confirm.label ?? m.common_confirm(),
			destructive: confirm.destructive ?? false,
			button: confirm.button,
			action: confirm.action
		},
		checkboxes
	}));
}
